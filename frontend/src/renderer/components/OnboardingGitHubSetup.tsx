import { Check, GitPullRequest, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useGitHubSetup } from "../hooks/useGitHubSetup";
import { AuthTerminalPanel } from "./AuthTerminalPanel";

/** GitHub readiness inside first-run setup. The board's notice sends a missing
 *  CLI to a docs page and only offers a sign-in terminal once gh exists; here
 *  the install happens in place, then the sign-in, so a first-run user
 *  finishes both without leaving the flow. */
export function OnboardingGitHubSetup() {
	const { t } = useTranslation();
	const setup = useGitHubSetup();

	if (setup.authSatisfied) {
		return (
			<p className="flex items-center justify-center gap-2 text-xs text-muted-foreground" role="status">
				<Check aria-hidden="true" className="size-3.5 text-status-ready" />
				{t("startup.githubConnected")}
			</p>
		);
	}
	if (!setup.gh) return null;

	const installFailed = setup.job?.status === "failed" || setup.job?.status === "unsupported" || setup.job?.status === "interrupted";
	const installDetail = setup.installError ?? (installFailed ? setup.job?.error : undefined);

	return (
		<div className="w-full max-w-[520px] rounded-lg border border-border bg-card px-3.5 py-3 text-left">
			<div className="flex items-start gap-2.5">
				<span className="mt-0.5 grid size-6 shrink-0 place-items-center rounded-md bg-interactive-hover text-muted-foreground">
					<GitPullRequest className="size-3.5" aria-hidden="true" />
				</span>
				<div className="min-w-0 flex-1">
					<p className="text-sm font-medium text-foreground">{t("startup.githubSetupTitle")}</p>
					<p className="mt-0.5 text-caption leading-snug text-muted-foreground">
						{t(setup.cliMissing ? "startup.githubSetupMissingCli" : "startup.githubSetupSignedOut")}
					</p>
					<div className="mt-2 flex flex-wrap items-center gap-2">
						{setup.cliMissing ? (
							<button
								type="button"
								onClick={() => void setup.install()}
								disabled={setup.installing}
								className="inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground hover:opacity-85 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-progress disabled:opacity-60"
							>
								{setup.installing ? <Loader2 aria-hidden="true" className="size-3.5 animate-spin motion-reduce:animate-none" /> : null}
								{setup.installing ? t("startup.installingEllipsis") : installFailed ? t("onboarding.tryAgain") : t("startup.installGh")}
							</button>
						) : (
							<button
								type="button"
								onClick={setup.signIn}
								disabled={setup.signInPending || setup.loginRunning || Boolean(setup.workflow)}
								aria-label={t("startup.githubLogin")}
								className="inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground hover:opacity-85 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60"
							>
								{setup.signInPending ? t("startup.githubLoginStarting") : setup.loginEnded ? t("startup.githubLoginTryAgain") : t("startup.githubLogin")}
							</button>
						)}
						{setup.loginRunning ? null : (
							<button
								type="button"
								onClick={() => void setup.requirementsQuery.refetch()}
								className="inline-flex h-8 items-center rounded-md border border-border px-3 text-xs text-muted-foreground hover:bg-interactive-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
							>
								{t("startup.checkAgain")}
							</button>
						)}
					</div>
					{installDetail ? <p className="mt-2 text-caption leading-snug text-warning" role="status">{installDetail}</p> : null}
					{setup.signInError ? <p className="mt-2 text-caption leading-snug text-destructive" role="alert">{setup.signInError}</p> : null}
					{setup.workflow ? (
						<AuthTerminalPanel
							workflow={setup.workflow}
							onClose={setup.closeSignIn}
							onRetry={setup.signIn}
							onTerminalState={setup.handleTerminalState}
							closeLabel={t("common.close")}
							testId="github-auth-terminal"
						/>
					) : null}
				</div>
			</div>
		</div>
	);
}
