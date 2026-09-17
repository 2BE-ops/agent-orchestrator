import { Check, GitPullRequest, Loader2, TerminalSquare } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { useGitHubSetup } from "../hooks/useGitHubSetup";
import { AuthTerminalPanel } from "./AuthTerminalPanel";
import { SetupActionButton } from "./SetupActionButton";

/** Step: GitHub. One page handles both halves of the prerequisite, because
 *  installing the CLI and signing in are one intention. The checks run from
 *  the first step's mount, so this page opens already knowing the state. */
export function OnboardingGitHubStep({ setup }: { setup: ReturnType<typeof useGitHubSetup> }) {
	const { t } = useTranslation();
	const installFailed = setup.job?.status === "failed" || setup.job?.status === "unsupported" || setup.job?.status === "interrupted";
	const installDetail = setup.installError ?? (installFailed ? setup.job?.error : undefined);

	if (!setup.gh) {
		return (
			<p className="px-1 text-caption text-muted-foreground" role="status">
				{t("onboarding.checkingAvailability")}
			</p>
		);
	}

	if (setup.authSatisfied) {
		return (
			<div className="flex w-full max-w-[420px] flex-col gap-2 text-left">
				<SetupActionButton
					icon={<Check className="text-status-ready" aria-hidden="true" />}
					label={t("startup.githubConnected")}
					description={t("onboarding.githubConnectedDetail")}
					disabled
				/>
			</div>
		);
	}

	return (
		<div className="flex w-full max-w-[420px] flex-col gap-2 text-left">
			{setup.cliMissing ? (
				<SetupActionButton
					icon={<TerminalSquare aria-hidden="true" />}
					label={installFailed ? t("onboarding.tryAgain") : t("startup.installGh")}
					description={t("startup.githubSetupMissingCli")}
					disabled={setup.installing}
					onClick={() => void setup.install()}
					trailing={setup.installing ? <Loader2 aria-hidden="true" className="size-3.5 animate-spin motion-reduce:animate-none" /> : undefined}
				/>
			) : (
				<SetupActionButton
					icon={<GitPullRequest aria-hidden="true" />}
					label={setup.loginEnded ? t("startup.githubLoginTryAgain") : t("startup.githubLogin")}
					description={t("startup.githubSetupSignedOut")}
					disabled={setup.signInPending || setup.loginRunning}
					onClick={setup.signIn}
					trailing={setup.signInPending || setup.loginRunning ? <Loader2 aria-hidden="true" className="size-3.5 animate-spin motion-reduce:animate-none" /> : undefined}
				/>
			)}
			{installDetail ? (
				<p className="px-4 text-caption leading-snug text-warning" role="status">
					{installDetail}
				</p>
			) : null}
			{setup.signInError ? (
				<p className="px-4 text-caption leading-snug text-destructive" role="alert">
					{setup.signInError}
				</p>
			) : null}
			{setup.loginRunning ? null : (
				<button
					type="button"
					onClick={() => void setup.requirementsQuery.refetch()}
					className="mt-1 self-start px-1 text-caption text-muted-foreground underline-offset-2 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
				>
					{t("startup.checkAgain")}
				</button>
			)}
			{setup.workflow ? (
				<AuthTerminalPanel
					workflow={setup.workflow}
					onClose={setup.closeSignIn}
					onRetry={setup.signIn}
					onTerminalState={setup.handleTerminalState}
					closeLabel={t("common.close")}
					terminalHeightClass="h-[200px]"
					testId="github-auth-terminal"
				/>
			) : null}
		</div>
	);
}
