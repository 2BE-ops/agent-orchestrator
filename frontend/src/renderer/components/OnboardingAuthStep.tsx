import { Check, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { useHarnessSetup } from "../hooks/useHarnessSetup";
import { AuthTerminalPanel } from "./AuthTerminalPanel";
import { SetupActionButton } from "./SetupActionButton";

export type OnboardingAuthAgent = {
	id: string;
	installed: boolean;
	name: string;
	indicator: "auth" | "checking" | "none";
	iconUrl?: string;
};

/** Step: a working agent. Install or sign in to one harness here, because a
 *  session cannot run until an agent CLI is both present and authenticated. */
export function OnboardingAuthStep({ agents, setup, onInstalled, onSignedIn }: {
	agents: OnboardingAuthAgent[];
	setup: ReturnType<typeof useHarnessSetup>;
	onInstalled: (agentId: string) => void;
	onSignedIn: (agentId: string) => void;
}) {
	const { t } = useTranslation();
	const visible = agents.filter((agent) => agent.indicator !== "checking").slice(0, 8);
	const checking = agents.length > 0 && agents.every((agent) => agent.indicator === "checking");

	return (
		<div className="flex w-full max-w-[420px] flex-col gap-2 text-left">
			{checking ? (
				<p className="flex items-center gap-2 px-4 py-3 text-caption text-muted-foreground" role="status">
					<Loader2 aria-hidden="true" className="size-3.5 animate-spin motion-reduce:animate-none" />
					{t("onboarding.checkingAvailability")}
				</p>
			) : null}
			{visible.map((agent) => {
				const plan = setup.authPlanFor(agent.id);
				const installing = setup.isInstalling(agent.id);
				const job = setup.jobFor(agent.id);
				const failed = job?.status === "failed" || job?.status === "unsupported" || job?.status === "interrupted";
				const error = setup.actionErrors[agent.id] ?? (failed ? job?.error : undefined);
				const stateLabel = !agent.installed
					? installing
						? t("onboarding.installing")
						: failed
							? t("onboarding.tryAgain")
							: t("onboarding.install")
					: agent.indicator === "auth"
						? t("onboarding.signIn")
						: t("onboarding.authSignedIn");

				return (
					<div key={agent.id} className="flex flex-col">
						<SetupActionButton
							icon={agent.iconUrl ? <img src={agent.iconUrl} alt="" className="size-4 object-contain" /> : null}
							label={agent.name}
							ariaLabel={
								!agent.installed
									? installing
										? t("onboarding.installingAgent", { agent: agent.name })
										: failed
											? t("onboarding.tryAgainToInstallAgent", { agent: agent.name })
											: t("onboarding.installAgent", { agent: agent.name })
									: agent.indicator === "auth" && plan?.available
										? t("onboarding.signInToAgent", { agent: agent.name })
										: agent.name
							}
							disabled={installing || (agent.installed && (agent.indicator !== "auth" || !plan?.available))}
							onClick={() => {
								if (!agent.installed) {
									void setup.startInstall(agent.id).then(() => onInstalled(agent.id));
									return;
								}
								void setup.startAuth(agent.id).then(() => onSignedIn(agent.id));
							}}
							trailing={
								installing ? (
									<Loader2 aria-hidden="true" className="size-3.5 animate-spin motion-reduce:animate-none" />
								) : agent.installed && agent.indicator !== "auth" ? (
									<Check aria-hidden="true" className="size-3.5 text-status-ready" />
								) : (
									stateLabel
								)
							}
						/>
						{error ? (
							<p className="px-4 pt-1 text-caption leading-snug text-warning" role="status">
								{error}
							</p>
						) : null}
					</div>
				);
			})}
			<p className="mt-1 px-1 text-caption leading-snug text-muted-foreground">
				{t("onboarding.authNudge")}
			</p>
			{setup.authWorkflow ? (
				<AuthTerminalPanel
					workflow={setup.authWorkflow}
					onClose={() => void setup.closeAuth()}
					onRetry={() => void setup.retryAuth()}
					onTerminalState={setup.handleTerminalState}
					closeLabel={t("common.close")}
					terminalHeightClass="h-[200px]"
				/>
			) : null}
		</div>
	);
}
