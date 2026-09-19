import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	defaultMetricsWindow,
	getManagerRoutingSummary,
	getOrchestratorPlanningSummary,
	getTaskPerformanceSummary,
	listTaskPerformance,
	performanceQueryRoot,
	type ManagerRoutingSummary,
	type OrchestratorPlanningSummary,
	type PerformanceGroupBy,
	type TaskPerformanceAttempt,
	type TaskPerformanceSummary,
} from "../lib/adaptive-api";
import { apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

const fieldClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary";

const groupByOptions: PerformanceGroupBy[] = [
	"agent_type",
	"agent_type_version",
	"skill",
	"skill_version",
	"harness",
	"model",
	"category",
	"capability",
];

function toLocalInput(iso: string): string {
	const date = new Date(iso);
	const pad = (value: number) => String(value).padStart(2, "0");
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function outcomeTone(outcome: string): string {
	switch (outcome) {
		case "passed":
			return "border-emerald-500/50 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
		case "failed":
			return "border-destructive/50 bg-destructive/10 text-destructive";
		case "inconclusive":
			return "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400";
		case "superseded":
			return "border-border bg-muted text-muted-foreground";
		default:
			return "border-border bg-background text-muted-foreground";
	}
}

function formatTokens(value: number | null | undefined): string {
	if (value === null || value === undefined) return "—";
	return value.toLocaleString();
}

function UsageFacts({ attempt }: { attempt: TaskPerformanceAttempt }) {
	const { t } = useTranslation();
	const usage = attempt.usage;
	if (!usage.events && !usage.inputTokens && !usage.outputTokens) return null;
	return (
		<span className="text-muted-foreground">
			{" · "}
			{t("perf.usageTokensFacts", "{{label}}: {{input}} in / {{output}} out", {
				label: t("perf.tokens", "tokens"),
				input: formatTokens(usage.inputTokens),
				output: formatTokens(usage.outputTokens),
			})}
			{usage.incomplete ? " (!)" : ""}
		</span>
	);
}

function SummaryTotals({ summary }: { summary: TaskPerformanceSummary }) {
	const { t } = useTranslation();
	const total = summary.total;
	const facts: Array<[string, string | number]> = [
		[t("perf.attempts", "Attempts"), total.attempts],
		[t("perf.passed", "Assessed passed"), total.assessedPassed],
		[t("perf.failed", "Assessed failed"), total.assessedFailed],
		[t("perf.inconclusive", "Inconclusive"), total.inconclusive],
		[t("perf.unassessed", "Unassessed"), total.unassessed],
		[t("perf.superseded", "Superseded"), total.superseded],
		[t("perf.firstPass", "First-pass completed"), total.firstPassCompleted],
		[t("perf.retries", "Retry attempts"), total.retryAttempts],
		[t("perf.ciFailures", "CI failure attempts"), total.ciFailureAttempts],
		[t("perf.reviewChanges", "Review changes requested"), total.reviewChangesRequested],
		[t("perf.usageTokens", "Session usage (in/out)"), `${formatTokens(total.usage.inputTokens)} / ${formatTokens(total.usage.outputTokens)}`],
	];
	return (
		<dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
			{facts.map(([label, value]) => (
				<div key={label} className="contents">
					<dt className="text-muted-foreground">{label}</dt>
					<dd className="text-right font-medium tabular-nums">{value}</dd>
				</div>
			))}
		</dl>
	);
}

function RoutingSummaryCard({ summary }: { summary: ManagerRoutingSummary }) {
	const { t } = useTranslation();
	const states = summary.totals.routedTaskStates;
	return (
		<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("perf.routingSummary", "Routing decisions summary")}>
			<header className="flex items-center justify-between gap-2">
				<h2 className="text-sm font-semibold">{t("perf.routingSummary", "Routing decisions summary")}</h2>
				<Badge className="border-border bg-muted text-muted-foreground">
					{t("perf.decisions", "{{total}} decisions", { total: summary.totals.decisions })}
				</Badge>
			</header>
			<dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
				{([
					[t("perf.accepted", "Accepted"), summary.totals.accepted],
					[t("perf.rejected", "Rejected"), summary.totals.rejected],
					[t("perf.routedCompleted", "Routed completed"), states.completed],
					[t("perf.routedFailed", "Routed failed"), states.failed],
					[t("perf.routedWorking", "Routed working or pending"), states.working + states.pending],
					[t("perf.routedCancelled", "Routed cancelled"), states.cancelled + states.cancelling],
					[t("perf.routedAttempts", "Routed task attempts"), summary.totals.routedTaskAttempts],
				] as Array<[string, number]>).map(([label, value]) => (
					<div key={label} className="contents">
						<dt className="text-muted-foreground">{label}</dt>
						<dd className="text-right font-medium tabular-nums">{value}</dd>
					</div>
				))}
			</dl>
			{summary.types.length === 0 ? (
				<p className="text-sm text-muted-foreground">{t("perf.noTypeGroups", "No accepted routing in this window.")}</p>
			) : (
				<table className="w-full text-left text-sm">
					<thead className="text-xs uppercase tracking-wide text-muted-foreground">
						<tr>
							<th scope="col" className="py-1 pr-4 font-medium">{t("perf.typeVersion", "Agent Type (version)")}</th>
							<th scope="col" className="py-1 pr-4 text-right font-medium">{t("perf.routed", "Routed")}</th>
							<th scope="col" className="py-1 pr-4 text-right font-medium">{t("perf.completed", "Completed")}</th>
							<th scope="col" className="py-1 pr-4 text-right font-medium">{t("perf.failedCol", "Failed")}</th>
							<th scope="col" className="py-1 text-right font-medium">{t("perf.openCol", "Open")}</th>
						</tr>
					</thead>
					<tbody>
						{summary.types.map((group) => (
							<tr key={`${group.agentTypeId}-${group.version}`} className="border-t border-border">
								<td className="py-1.5 pr-4">{group.name} <span className="text-muted-foreground">v{group.version}</span></td>
								<td className="py-1.5 pr-4 text-right tabular-nums">{group.routed}</td>
								<td className="py-1.5 pr-4 text-right tabular-nums">{group.completed}</td>
								<td className="py-1.5 pr-4 text-right tabular-nums">{group.failed}</td>
								<td className="py-1.5 text-right tabular-nums">{group.open}</td>
							</tr>
						))}
					</tbody>
				</table>
			)}
		</section>
	);
}

function PlanningSummaryCard({ summary }: { summary: OrchestratorPlanningSummary }) {
	const { t } = useTranslation();
	const states = summary.totals.plannedTaskStates;
	return (
		<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("perf.planningSummary", "Planning summary")}>
			<header className="flex items-center justify-between gap-2">
				<h2 className="text-sm font-semibold">{t("perf.planningSummary", "Planning summary")}</h2>
				<Badge className="border-border bg-muted text-muted-foreground">
					{t("perf.receipts", "{{total}} receipts", { total: summary.totals.receipts })}
				</Badge>
			</header>
			<dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
				{([
					[t("perf.createTask", "Create-task receipts"), summary.totals.createTask],
					[t("perf.reviseTask", "Revise-task receipts"), summary.totals.reviseTask],
					[t("perf.freezeCriteria", "Freeze-criteria receipts"), summary.totals.freezeCriteria],
					[t("perf.plannedCompleted", "Planned tasks completed"), states.completed],
					[t("perf.plannedFailed", "Planned tasks failed"), states.failed],
					[t("perf.plannedOpen", "Planned tasks open"), states.working + states.pending],
					[t("perf.plannedAttempts", "Planned task attempts"), summary.totals.plannedTaskAttempts],
				] as Array<[string, number]>).map(([label, value]) => (
					<div key={label} className="contents">
						<dt className="text-muted-foreground">{label}</dt>
						<dd className="text-right font-medium tabular-nums">{value}</dd>
					</div>
				))}
			</dl>
		</section>
	);
}

export function PerformanceMetricsView({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const initial = defaultMetricsWindow();
	const [draftFrom, setDraftFrom] = useState(() => toLocalInput(initial.from));
	const [draftTo, setDraftTo] = useState(() => toLocalInput(initial.to));
	const [window, setWindow] = useState(initial);
	const [groupBy, setGroupBy] = useState<PerformanceGroupBy>("agent_type_version");
	const draftValid = new Date(draftFrom).getTime() < new Date(draftTo).getTime();

	const summary = useQuery({
		queryKey: [...performanceQueryRoot, projectId, "summary", window.from, window.to, groupBy],
		queryFn: () => getTaskPerformanceSummary(projectId, window.from, window.to, groupBy),
	});
	const routing = useQuery({
		queryKey: [...performanceQueryRoot, projectId, "routing-summary", window.from, window.to],
		queryFn: () => getManagerRoutingSummary(projectId, window.from, window.to),
	});
	const planning = useQuery({
		queryKey: [...performanceQueryRoot, projectId, "planning-summary", window.from, window.to],
		queryFn: () => getOrchestratorPlanningSummary(projectId, window.from, window.to),
	});
	const attempts = useInfiniteQuery({
		queryKey: [...performanceQueryRoot, projectId, "attempts", window.from, window.to],
		queryFn: ({ pageParam }) => listTaskPerformance(projectId, window.from, window.to, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const attemptRows = attempts.data?.pages.flatMap((page) => page.items) ?? [];

	const applyWindow = () => {
		if (!draftValid) return;
		setWindow({ from: new Date(draftFrom).toISOString(), to: new Date(draftTo).toISOString() });
	};

	return (
		<main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("perf.title", "Performance and attribution")}>
			<header className="mb-4 space-y-3">
				<h1 className="text-lg font-semibold">{t("perf.title", "Performance and attribution")}</h1>
				<div className="flex flex-wrap items-end gap-3" role="group" aria-label={t("perf.window", "Admission window")}>
					<label className="grid gap-1.5 text-sm">
						<span className="text-muted-foreground">{t("perf.from", "From")}</span>
						<Input
							type="datetime-local"
							className={fieldClass}
							value={draftFrom}
							onChange={(event) => setDraftFrom(event.target.value)}
						/>
					</label>
					<label className="grid gap-1.5 text-sm">
						<span className="text-muted-foreground">{t("perf.to", "To")}</span>
						<Input
							type="datetime-local"
							className={fieldClass}
							value={draftTo}
							onChange={(event) => setDraftTo(event.target.value)}
						/>
					</label>
					<label className="grid gap-1.5 text-sm">
						<span className="text-muted-foreground">{t("perf.groupBy", "Group by")}</span>
						<select
							className={fieldClass}
							value={groupBy}
							onChange={(event) => setGroupBy(event.target.value as PerformanceGroupBy)}
							aria-label={t("perf.groupBy", "Group by")}
						>
							{groupByOptions.map((option) => (
								<option key={option} value={option}>{option}</option>
							))}
						</select>
					</label>
					<Button onClick={applyWindow} disabled={!draftValid || summary.isFetching}>
						{t("perf.apply", "Apply window")}
					</Button>
					{!draftValid ? (
						<p role="alert" className="text-sm text-destructive">{t("perf.invalidWindow", "From must be before to.")}</p>
					) : null}
				</div>
				<p className="text-xs text-muted-foreground">
					{t("perf.windowNote", "Windows select attempt admissions (up to 366 days) and are aggregated as one complete cohort; counts are retained, not percentages.")}
				</p>
			</header>

			<div className="grid gap-4">
				<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("perf.summary", "Cohort summary")}>
					<h2 className="text-sm font-semibold">{t("perf.summary", "Cohort summary")}</h2>
					{summary.isPending ? (
						<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
					) : summary.isError ? (
						<div role="alert" className="space-y-2">
							<p className="text-sm text-destructive">{apiErrorMessage(summary.error)}</p>
							<Button variant="outline" onClick={() => summary.refetch()}>{t("common.retry", "Retry")}</Button>
						</div>
					) : summary.data ? (
						<div className="space-y-3">
							<SummaryTotals summary={summary.data} />
							{summary.data.groups.length === 0 ? (
								<p className="text-sm text-muted-foreground">{t("perf.noGroups", "No admitted attempts in this window.")}</p>
							) : (
								<table className="w-full text-left text-sm">
									<thead className="text-xs uppercase tracking-wide text-muted-foreground">
										<tr>
											<th scope="col" className="py-1 pr-4 font-medium">{t("perf.group", "Group")}</th>
											<th scope="col" className="py-1 pr-4 text-right font-medium">{t("perf.attempts", "Attempts")}</th>
											<th scope="col" className="py-1 pr-4 text-right font-medium">{t("perf.passed", "Assessed passed")}</th>
											<th scope="col" className="py-1 pr-4 text-right font-medium">{t("perf.failedCol", "Failed")}</th>
											<th scope="col" className="py-1 text-right font-medium">{t("perf.firstPass", "First-pass completed")}</th>
										</tr>
									</thead>
									<tbody>
										{summary.data.groups.map((group) => (
											<tr key={group.version ? `${group.key}-v${group.version}` : group.key} className="border-t border-border">
												<td className="py-1.5 pr-4">
													{group.name}
													{group.version ? <span className="text-muted-foreground"> v{group.version}</span> : null}
												</td>
												<td className="py-1.5 pr-4 text-right tabular-nums">{group.metrics.attempts}</td>
												<td className="py-1.5 pr-4 text-right tabular-nums">{group.metrics.assessedPassed}</td>
												<td className="py-1.5 pr-4 text-right tabular-nums">{group.metrics.assessedFailed}</td>
												<td className="py-1.5 text-right tabular-nums">{group.metrics.firstPassCompleted}</td>
											</tr>
										))}
									</tbody>
								</table>
							)}
							{summary.data.excludedMixedConfigurationAttempts > 0 || summary.data.excludedUnseededAttempts > 0 ? (
								<p className="text-xs text-muted-foreground">
									{t("perf.exclusions", "Configuration groups exclude {{mixed}} mixed-configuration and {{unseeded}} unseeded attempts.", {
										mixed: summary.data.excludedMixedConfigurationAttempts,
										unseeded: summary.data.excludedUnseededAttempts,
									})}
								</p>
							) : null}
						</div>
					) : null}
				</section>

				<div className="grid gap-4 xl:grid-cols-2">
					{routing.isPending ? (
						<section className="rounded-lg border border-border p-4" aria-label={t("perf.routingSummary", "Routing decisions summary")}>
							<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
						</section>
					) : routing.isError ? (
						<section className="rounded-lg border border-border p-4" aria-label={t("perf.routingSummary", "Routing decisions summary")}>
							<div role="alert" className="space-y-2">
								<p className="text-sm text-destructive">{apiErrorMessage(routing.error)}</p>
								<Button variant="outline" onClick={() => routing.refetch()}>{t("common.retry", "Retry")}</Button>
							</div>
						</section>
					) : routing.data ? <RoutingSummaryCard summary={routing.data.summary} /> : null}

					{planning.isPending ? (
						<section className="rounded-lg border border-border p-4" aria-label={t("perf.planningSummary", "Planning summary")}>
							<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
						</section>
					) : planning.isError ? (
						<section className="rounded-lg border border-border p-4" aria-label={t("perf.planningSummary", "Planning summary")}>
							<div role="alert" className="space-y-2">
								<p className="text-sm text-destructive">{apiErrorMessage(planning.error)}</p>
								<Button variant="outline" onClick={() => planning.refetch()}>{t("common.retry", "Retry")}</Button>
							</div>
						</section>
					) : planning.data ? <PlanningSummaryCard summary={planning.data.summary} /> : null}
				</div>

				<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("perf.evidence", "Attempt evidence")}>
					<h2 className="text-sm font-semibold">{t("perf.evidence", "Attempt evidence")}</h2>
					{attempts.isPending ? (
						<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
					) : attempts.isError ? (
						<div role="alert" className="space-y-2">
							<p className="text-sm text-destructive">{apiErrorMessage(attempts.error)}</p>
							<Button variant="outline" onClick={() => attempts.refetch()}>{t("common.retry", "Retry")}</Button>
						</div>
					) : attemptRows.length === 0 ? (
						<p className="text-sm text-muted-foreground">{t("perf.noAttempts", "No attempts were admitted in this window.")}</p>
					) : (
						<>
							<ul className="space-y-2" aria-label={t("perf.evidenceList", "Attempt evidence list")}>
								{attemptRows.map((attempt) => (
									<li key={attempt.attemptId} className="rounded-md border border-border p-3 text-sm">
										<div className="flex flex-wrap items-center gap-2">
											<Badge className={cn("border", outcomeTone(attempt.assessedOutcome))}>{attempt.assessedOutcome}</Badge>
											<span className="font-medium">
												{t("perf.attemptLabel", "Attempt {{number}}", { number: attempt.attemptNumber })}
											</span>
											<span className="text-muted-foreground">{attempt.taskId}</span>
											{attempt.configuration ? (
												<span className="text-muted-foreground">
													{attempt.configuration.agentType.name}
													{" v"}
													{attempt.configuration.agentType.version}
													{" · "}
													{attempt.configuration.model}
												</span>
											) : (
												<span className="text-muted-foreground">{t("perf.unseeded", "Unseeded reservation")}</span>
											)}
										</div>
										<p className="mt-1 text-muted-foreground">
											{new Date(attempt.createdAt).toLocaleString()}
											{attempt.firstPassCompleted ? ` · ${t("perf.firstPassShort", "first pass completed")}` : ""}
											{attempt.ciFailureObserved ? ` · ${t("perf.ciObserved", "CI failure observed")}` : ""}
											{attempt.reviewChangesRequested > 0 ? ` · ${t("perf.changesShort", "{{total}} change requests", { total: attempt.reviewChangesRequested })}` : ""}
											<UsageFacts attempt={attempt} />
										</p>
									</li>
								))}
							</ul>
							{attempts.hasNextPage ? (
								<Button variant="outline" disabled={attempts.isFetchingNextPage} onClick={() => attempts.fetchNextPage()}>
									{attempts.isFetchingNextPage ? t("common.loadingMore", "Loading…") : t("common.loadMore", "Load more")}
								</Button>
							) : null}
						</>
					)}
				</section>
			</div>
		</main>
	);
}
