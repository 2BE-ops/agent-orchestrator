import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { components } from "../../api/schema";
import type { AuthWorkflow } from "../components/AuthTerminalPanel";
import { useCloseShellTerminal } from "./useShellTerminals";
import { isActiveInstallJob } from "./useHarnessSetup";
import type { TerminalSessionState } from "./useTerminalSession";
import { useGitHubAuthRequirement, useGitHubAuthTerminal, useStartGitHubAuthTerminal, useSystemRequirementsGate } from "./useSystemRequirementsGate";
import { apiClient, apiErrorMessage } from "../lib/api-client";

type InstallJob = components["schemas"]["InstallJob"];

const GH_INSTALL_TARGET = "gh" as const;
const POLL_INTERVAL_MS = 1_000;
/** The GitHub page watches long-running external work (a package install, a
 *  device-code sign-in), so it polls slowly rather than every second. */
const STEP_POLL_INTERVAL_MS = 2_500;

/**
 * GitHub readiness for onboarding: install the CLI in place when it is missing,
 * then run the daemon-owned `gh auth login` terminal when it is present but
 * signed out. GitHub stays advisory, so nothing here blocks the rest of setup.
 */
export function useGitHubSetup({ poll = false }: { poll?: boolean } = {}) {
	const { t } = useTranslation();
	const gate = useSystemRequirementsGate();
	const terminalQuery = useGitHubAuthTerminal();
	const startSignIn = useStartGitHubAuthTerminal();
	const { mutate: closeTerminal } = useCloseShellTerminal();
	const [terminalState, setTerminalState] = useState<{ handleId: string | null; state: TerminalSessionState }>({
		handleId: null,
		state: "idle",
	});
	const [job, setJob] = useState<InstallJob | undefined>(undefined);
	const [installing, setInstalling] = useState(false);
	const [installError, setInstallError] = useState<string | null>(null);
	const pollRef = useRef<number | null>(null);

	const terminal = terminalQuery.data;
	const resolvedTerminalState = terminalState.handleId === terminal?.handleId ? terminalState.state : "idle";
	const loginRunning = Boolean(terminal && (resolvedTerminalState === "connecting" || resolvedTerminalState === "attached" || resolvedTerminalState === "reattaching"));
	const loginEnded = Boolean(terminal && (resolvedTerminalState === "exited" || resolvedTerminalState === "error"));
	const gh = gate.requirements.find((requirement) => requirement.id === GH_INSTALL_TARGET);
	const auth = useGitHubAuthRequirement(loginRunning);
	const authRef = useRef(auth.refetch);
	authRef.current = auth.refetch;
	const requirementsRef = useRef(gate.query.refetch);
	requirementsRef.current = gate.query.refetch;
	const stopPolling = useCallback(() => {
		if (pollRef.current !== null) {
			window.clearInterval(pollRef.current);
			pollRef.current = null;
		}
	}, []);
	useEffect(() => stopPolling, [stopPolling]);

	// No manual re-check on the GitHub page: it polls until both halves settle.
	// The CLI half matters while gh is missing (an install can land at any time)
	// and the auth half while GitHub is still signed out (a device flow can
	// finish after its terminal is gone).
	const cliMissing = gh?.satisfied === false;
	const authSatisfied = Boolean(auth.data?.satisfied);
	// "Not yet confirmed" is not the same as "needs nothing": a probe that ran
	// before the daemon was reachable leaves gh unknown, and that has to keep
	// polling or the page sits on its checking state forever.
	const cliReady = gh?.satisfied === true;
	useEffect(() => {
		if (!poll || (cliReady && authSatisfied)) return;
		const timer = window.setInterval(() => {
			// Each half is only worth probing while it can still change.
			if (!cliReady) void requirementsRef.current();
			if (!authSatisfied) void authRef.current();
		}, STEP_POLL_INTERVAL_MS);
		return () => window.clearInterval(timer);
	}, [authSatisfied, cliReady, poll]);

	const pollInstall = useCallback(() => {
		stopPolling();
		pollRef.current = window.setInterval(() => {
			void (async () => {
				const { data, error } = await apiClient.GET("/api/v1/system/install/{target}", {
					params: { path: { target: GH_INSTALL_TARGET } },
				});
				if (error || !data) return;
				setJob(data);
				if (isActiveInstallJob(data)) return;
				stopPolling();
				if (data.status === "succeeded") void gate.query.refetch();
			})();
		}, POLL_INTERVAL_MS);
	}, [gate.query, stopPolling]);

	const install = useCallback(async () => {
		setInstalling(true);
		setInstallError(null);
		try {
			const { data, error } = await apiClient.POST("/api/v1/system/install/{target}", {
				params: { path: { target: GH_INSTALL_TARGET } },
			});
			if (error || !data) throw new Error(apiErrorMessage(error, t("onboarding.installStartFailed")));
			setJob(data);
			if (isActiveInstallJob(data)) pollInstall();
			else if (data.status === "succeeded") await gate.query.refetch();
		} catch (error) {
			setInstallError(error instanceof Error ? error.message : t("onboarding.installStartFailed"));
		} finally {
			setInstalling(false);
		}
	}, [gate.query, pollInstall, t]);

	const handleTerminalState = useCallback((state: TerminalSessionState) => {
		setTerminalState({ handleId: terminalQuery.data?.handleId ?? null, state });
		if (state !== "exited" && state !== "error") return;
		void authRef.current();
	}, [terminalQuery.data?.handleId]);

	const signIn = useCallback(() => {
		startSignIn.mutate();
	}, [startSignIn]);

	const closeSignIn = useCallback(() => {
		if (!terminal) return;
		closeTerminal(terminal.handleId, {
			onSuccess: () => {
				terminalQuery.clear();
				void authRef.current();
			},
		});
	}, [closeTerminal, terminal, terminalQuery]);

	// A device flow can finish in the terminal before its PTY exit reaches the
	// renderer. When the requirement flips, retire the terminal and let the
	// surface collapse on its own.
	const completedRef = useRef<string | null>(null);
	useEffect(() => {
		if (!auth.data?.satisfied || !terminal) return;
		if (completedRef.current === terminal.handleId) return;
		completedRef.current = terminal.handleId;
		closeTerminal(terminal.handleId, {
			onSuccess: () => {
				terminalQuery.clear();
				void gate.query.refetch();
			},
		});
	}, [auth.data?.satisfied, closeTerminal, gate.query, terminal, terminalQuery]);

	const workflow: AuthWorkflow | null = terminal
		? {
				agentId: "github",
				action: "login",
				terminal,
				guidance: t(loginEnded ? "startup.githubLoginStopped" : "startup.githubLoginRunning"),
				phase: loginEnded ? "unauthorized" : "running",
				reason: loginEnded ? t("startup.githubLoginStopped") : undefined,
				startedAt: Date.parse(terminal.createdAt) || Date.now(),
			}
		: null;

	return {
		authSatisfied,
		cliMissing,
		closeSignIn,
		gh,
		handleTerminalState,
		install,
		installError,
		installing,
		job,
		loginEnded,
		loginRunning,
		requirementsQuery: gate.query,
		signIn,
		signInError: startSignIn.isError ? startSignIn.error.message : null,
		signInPending: startSignIn.isPending,
		workflow,
	};
}
