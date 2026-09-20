import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
	getManagerController,
	listManagerConfigurations,
	listProjectActiveSessions,
	listRoutingOutcomes,
	managerDashboardQueryRoot,
	workerSessionsOf,
	type ManagerConfiguration,
	type RoutingOutcome,
} from "../lib/adaptive-api";
import { apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";

function formatWhen(value: string | null | undefined): string {
	if (!value) return "—";
	const parsed = new Date(value);
	return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

export function AgentManagerDashboard({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const navigate = useNavigate();

	const controller = useQuery({
		queryKey: [...managerDashboardQueryRoot, projectId, "controller"],
		queryFn: () => getManagerController(projectId),
	});
	const configurations = useInfiniteQuery({
		queryKey: [...managerDashboardQueryRoot, projectId, "configurations"],
		queryFn: ({ pageParam }) => listManagerConfigurations(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const sessions = useQuery({
		queryKey: [...managerDashboardQueryRoot, projectId, "sessions"],
		queryFn: () => listProjectActiveSessions(projectId),
	});
	const decisions = useInfiniteQuery({
		queryKey: [...managerDashboardQueryRoot, projectId, "routing-outcomes"],
		queryFn: ({ pageParam }) => listRoutingOutcomes(projectId, pageParam),
		initialPageParam: 0,
		getNextPageParam: (page) => page.nextAfter || undefined,
	});

	const configurationItems = configurations.data?.pages.flatMap((page) => page.items) ?? [];
	// Configurations page oldest-first by version number, so the newest loaded
	// version is the governance a fresh admission would use.
	const active = configurationItems.at(-1);
	const workers = workerSessionsOf(sessions.data ?? []);
	const decisionItems = decisions.data?.pages.flatMap((page) => page.items) ?? [];

	const openSession = (sessionId: string) =>
		void navigate({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId, sessionId },
		});

	return (
		<main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("managerDash.title", "Agent Manager")}>
			<header className="mb-5">
				<h1 className="text-xl font-semibold">{t("managerDash.title", "Agent Manager")}</h1>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("managerDash.description", "Routing decisions, the current worker population and the governance the Manager runs under.")}
				</p>
			</header>

			<div className="grid gap-5 xl:grid-cols-2">
				<section className="rounded-lg border border-border p-4" aria-label={t("managerDash.status", "Controller status")}>
					<h2 className="mb-2 font-medium">{t("managerDash.status", "Controller status")}</h2>
					{controller.isPending && <p role="status">{t("managerDash.loading", "Loading…")}</p>}
					{controller.isError && (
						<div role="alert">
							{apiErrorMessage(controller.error)}{" "}
							<Button variant="outline" onClick={() => void controller.refetch()}>
								{t("common.retry", "Retry")}
							</Button>
						</div>
					)}
					{controller.data && controller.data.state === null && (
						<p className="text-sm text-muted-foreground">
							{t("managerDash.noController", "No native Manager is currently admitted for this project. The configured governance admits one automatically when routed work appears.")}
						</p>
					)}
					{controller.data?.state && (
						<dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
							<dt className="text-muted-foreground">{t("managerDash.controllerId", "Controller")}</dt>
							<dd className="break-all">{controller.data.state.controller.id}</dd>
							<dt className="text-muted-foreground">{t("managerDash.configurationVersion", "Configuration")}</dt>
							<dd>v{controller.data.state.controller.configurationVersion}</dd>
							<dt className="text-muted-foreground">{t("managerDash.admittedAt", "Admitted")}</dt>
							<dd>{formatWhen(controller.data.state.controller.createdAt)}</dd>
							<dt className="text-muted-foreground">{t("managerDash.state", "State")}</dt>
							<dd>
								{controller.data.state.controller.releasedAt
									? t("managerDash.released", "Released {{when}}", { when: formatWhen(controller.data.state.controller.releasedAt) })
									: t("managerDash.active", "Active")}
							</dd>
							{controller.data.state.dispatch && (
								<>
									<dt className="text-muted-foreground">{t("managerDash.dispatchSession", "Manager session")}</dt>
									<dd>
										<Button variant="ghost" className="h-auto p-0 underline" onClick={() => openSession(controller.data!.state!.dispatch!.sessionId)}>
											{t("managerDash.openSession", "Open session")}
										</Button>
									</dd>
								</>
							)}
							{controller.data.state.pendingOperation && (
								<>
									<dt className="text-muted-foreground">{t("managerDash.pendingOperation", "Pending operation")}</dt>
									<dd>{controller.data.state.pendingOperation.kind} · {formatWhen(controller.data.state.pendingOperation.createdAt)}</dd>
								</>
							)}
							<dt className="text-muted-foreground">{t("managerDash.reason", "Reason")}</dt>
							<dd className="col-span-2 break-words">{controller.data.state.controller.reason}</dd>
						</dl>
					)}
				</section>

				<section className="rounded-lg border border-border p-4" aria-label={t("managerDash.governance", "Governance")}>
					<h2 className="mb-2 font-medium">{t("managerDash.governance", "Governance")}</h2>
					{configurations.isPending && <p role="status">{t("managerDash.loading", "Loading…")}</p>}
					{configurations.isError && (
						<div role="alert">
							{apiErrorMessage(configurations.error)}{" "}
							<Button variant="outline" onClick={() => void configurations.refetch()}>
								{t("common.retry", "Retry")}
							</Button>
						</div>
					)}
					{configurations.data && configurationItems.length === 0 && (
						<p className="text-sm text-muted-foreground">
							{t("managerDash.noConfiguration", "No Manager configuration recorded for this project yet.")}
						</p>
					)}
					{active && <ManagerConfigurationSummary configuration={active} />}
					{configurationItems.length > 1 && (
						<details className="mt-2">
							<summary className="cursor-pointer text-sm font-medium">
								{t("managerDash.configurationHistory", "Configuration history")}
							</summary>
							<ol className="mt-2 space-y-1 text-sm">
								{configurationItems.map((item) => (
									<li key={item.number}>
										v{item.number} · {formatWhen(item.createdAt)} · {item.reason}
									</li>
								))}
							</ol>
						</details>
					)}
					{configurations.hasNextPage && (
						<Button variant="outline" className="mt-2" disabled={configurations.isFetchingNextPage} onClick={() => void configurations.fetchNextPage()}>
							{t("managerDash.loadMore", "Load more")}
						</Button>
					)}
				</section>

				<section className="rounded-lg border border-border p-4" aria-label={t("managerDash.population", "Worker population")}>
					<h2 className="mb-2 font-medium">
						{t("managerDash.population", "Worker population")}{" "}
						<Badge>{t("managerDash.workers", "{{total}} active", { total: workers.length })}</Badge>
					</h2>
					{sessions.isPending && <p role="status">{t("managerDash.loading", "Loading…")}</p>}
					{sessions.isError && (
						<div role="alert">
							{apiErrorMessage(sessions.error)}{" "}
							<Button variant="outline" onClick={() => void sessions.refetch()}>
								{t("common.retry", "Retry")}
							</Button>
						</div>
					)}
					{sessions.data && workers.length === 0 && (
						<p className="text-sm text-muted-foreground">
							{t("managerDash.noWorkers", "No active workers. Routed tasks start appearing here once the Manager dispatches work.")}
						</p>
					)}
					<ul className="space-y-2">
						{workers.map((worker) => (
							<li key={worker.id} className="flex flex-wrap items-center gap-2 text-sm">
								<Button variant="ghost" className="h-auto p-0 text-left underline" onClick={() => openSession(worker.id)}>
									{worker.displayName || worker.id}
								</Button>
								<Badge>{worker.harness || t("managerDash.unknownHarness", "unknown harness")}</Badge>
								{worker.model && <span className="text-muted-foreground">{worker.model}</span>}
								<span className="text-muted-foreground">{worker.displayStatus}</span>
							</li>
						))}
					</ul>
				</section>

				<section className="rounded-lg border border-border p-4" aria-label={t("managerDash.decisions", "Routing decisions")}>
					<h2 className="mb-2 font-medium">{t("managerDash.decisions", "Routing decisions")}</h2>
					{decisions.isPending && <p role="status">{t("managerDash.loading", "Loading…")}</p>}
					{decisions.isError && (
						<div role="alert">
							{apiErrorMessage(decisions.error)}{" "}
							<Button variant="outline" onClick={() => void decisions.refetch()}>
								{t("common.retry", "Retry")}
							</Button>
						</div>
					)}
					{decisions.data && decisionItems.length === 0 && (
						<p className="text-sm text-muted-foreground">
							{t("managerDash.noDecisions", "No routing decisions recorded yet. The Manager seals one decision per request.")}
						</p>
					)}
					<ul className="space-y-3">
						{decisionItems.map((outcome) => (
							<RoutingDecisionRow key={outcome.decisionId} outcome={outcome} />
						))}
					</ul>
					{decisions.hasNextPage && (
						<Button variant="outline" className="mt-2" disabled={decisions.isFetchingNextPage} onClick={() => void decisions.fetchNextPage()}>
							{t("managerDash.loadMore", "Load more")}
						</Button>
					)}
				</section>
			</div>
		</main>
	);
}

function ManagerConfigurationSummary({ configuration }: { configuration: ManagerConfiguration }) {
	const { t } = useTranslation();
	const policy = configuration.definition.policy;
	return (
		<div className="space-y-2 text-sm">
			<dl className="grid grid-cols-2 gap-x-3 gap-y-1">
				<dt className="text-muted-foreground">{t("managerDash.controllerType", "Controller Type")}</dt>
				<dd className="break-all">
					{configuration.controllerType.name} · v{configuration.controllerType.version}
				</dd>
				<dt className="text-muted-foreground">{t("managerDash.enabled", "Enabled")}</dt>
				<dd>{configuration.definition.enabled ? t("common.yes", "Yes") : t("common.no", "No")}</dd>
				<dt className="text-muted-foreground">{t("managerDash.maxPending", "Maximum pending requests")}</dt>
				<dd>{policy.maxPendingRequests}</dd>
				<dt className="text-muted-foreground">{t("managerDash.maxCreatedTypes", "Maximum created Types")}</dt>
				<dd>{policy.maxCreatedTypes}</dd>
				<dt className="text-muted-foreground">{t("managerDash.maxCreatedSkills", "Maximum created Skills")}</dt>
				<dd>{policy.maxCreatedSkills}</dd>
			</dl>
			<div className="flex flex-wrap gap-1">
				{policy.allowCreateTypes && <Badge>{t("managerDash.canCreateTypes", "May create Agent Types")}</Badge>}
				{policy.allowCreateSkills && <Badge>{t("managerDash.canCreateSkills", "May create Skills")}</Badge>}
				{policy.allowCreateVersions && <Badge>{t("managerDash.canCreateVersions", "May create versions")}</Badge>}
			</div>
			<p className="text-muted-foreground">
				{t("managerDash.configuredAt", "Configured {{when}} · {{reason}}", { when: formatWhen(configuration.createdAt), reason: configuration.reason })}
			</p>
		</div>
	);
}

function RoutingDecisionRow({ outcome }: { outcome: RoutingOutcome }) {
	const { t } = useTranslation();
	const accepted = outcome.outcome === "accepted";
	return (
		<li className="border-l-2 border-border pl-3">
			<div className="flex flex-wrap items-center gap-2">
				<Badge
					className={cn(
						"border",
						accepted
							? "border-emerald-500/50 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
							: "border-destructive/50 bg-destructive/10 text-destructive",
					)}
				>
					{accepted ? t("managerDash.accepted", "Accepted") : t("managerDash.rejected", "Rejected")}
				</Badge>
				{outcome.agentType && (
					<span className="text-sm font-medium">
						{outcome.agentType.name} · v{outcome.agentType.version}
					</span>
				)}
				<span className="text-xs text-muted-foreground">{formatWhen(outcome.decidedAt)}</span>
			</div>
			<p className="mt-1 break-words text-sm">{outcome.reason}</p>
			<p className="mt-1 text-xs text-muted-foreground">
				{t("managerDash.routedTask", "Routed task")} {outcome.taskTitle} · {outcome.state} ·{" "}
				{t("managerDash.attempts", "{{total}} attempts", { total: outcome.attempts })}
			</p>
		</li>
	);
}
