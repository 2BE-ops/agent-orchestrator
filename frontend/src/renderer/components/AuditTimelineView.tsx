import { useInfiniteQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
	adaptiveAuditQueryRoot,
	adaptiveTasksQueryRoot,
	listManagerAudit,
	listProjectTasks,
	listTaskAudit,
} from "../lib/adaptive-api";
import { apiErrorMessage } from "../lib/api-client";
import { Button } from "./ui/button";

const fieldClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary";

function formatWhen(value: string | null | undefined): string {
	if (!value) return "—";
	const parsed = new Date(value);
	return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function actorLabel(actor: { kind: string; id: string }): string {
	return `${actor.kind}:${actor.id}`;
}

type AuditRow = {
	key: string;
	sequence: number;
	when: string;
	action: string;
	actor: { kind: string; id: string };
	reason: string;
	detail: string;
};

function AuditList({ rows, label }: { rows: AuditRow[]; label: string }) {
	return (
		<ol className="space-y-2 border-l border-border pl-4" aria-label={label}>
			{rows.map((row) => (
				<li key={row.key} className="relative rounded-md border border-border p-3 text-sm">
					<span aria-hidden className="absolute -left-[21px] top-4 h-2 w-2 rounded-full bg-primary" />
					<div className="flex flex-wrap items-center gap-2">
						<BadgeLike>{row.action}</BadgeLike>
						<span className="text-muted-foreground">{formatWhen(row.when)}</span>
						<span className="ml-auto text-xs text-muted-foreground">#{row.sequence}</span>
					</div>
					<p className="mt-1 text-muted-foreground">{row.detail}</p>
					{row.reason ? <p className="mt-1 text-xs text-muted-foreground">{row.reason}</p> : null}
					<p className="mt-1 text-xs text-muted-foreground">{actorLabel(row.actor)}</p>
				</li>
			))}
		</ol>
	);
}

function BadgeLike({ children }: { children: ReactNode }) {
	return (
		<span className="rounded-full border border-border bg-muted px-2 py-0.5 text-xs text-foreground">{children}</span>
	);
}

function LoadMore({ hasNextPage, isFetching, onClick, labels }: {
	hasNextPage: boolean;
	isFetching: boolean;
	onClick: () => void;
	labels: { more: string; loading: string };
}) {
	if (!hasNextPage) return null;
	return (
		<Button variant="outline" disabled={isFetching} onClick={onClick}>
			{isFetching ? labels.loading : labels.more}
		</Button>
	);
}

export function AuditTimelineView({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const [taskFilter, setTaskFilter] = useState("");

	const managerAudit = useInfiniteQuery({
		queryKey: [...adaptiveAuditQueryRoot, projectId, "manager"],
		queryFn: ({ pageParam }) => listManagerAudit(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const tasks = useInfiniteQuery({
		queryKey: [...adaptiveTasksQueryRoot, projectId, "list"],
		queryFn: ({ pageParam }) => listProjectTasks(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const taskAudit = useInfiniteQuery({
		queryKey: [...adaptiveAuditQueryRoot, projectId, "task", taskFilter],
		queryFn: ({ pageParam }) => listTaskAudit(taskFilter, pageParam),
		enabled: taskFilter !== "",
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});

	const managerRows: AuditRow[] = (managerAudit.data?.pages.flatMap((page) => page.items) ?? []).map((entry) => ({
		key: `manager-${entry.sequence}`,
		sequence: entry.sequence,
		when: entry.createdAt,
		action: entry.action,
		actor: entry.actor,
		reason: entry.reason,
		detail: t("audit.managerDetail", "Manager configuration v{{version}}", { version: entry.configurationVersion }),
	}));
	const taskRows: AuditRow[] = (taskAudit.data?.pages.flatMap((page) => page.items) ?? []).map((entry) => ({
		key: `task-${entry.taskId}-${entry.sequence}`,
		sequence: entry.sequence,
		when: entry.createdAt,
		action: entry.action,
		actor: entry.actor,
		reason: entry.reason,
		detail: t("audit.taskDetail", "Task revision v{{version}}", { version: entry.revision }),
	}));
	const taskOptions = tasks.data?.pages.flatMap((page) => page.items) ?? [];

	return (
		<main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("audit.title", "Chronological audit")}>
			<header className="mb-4">
				<h1 className="text-lg font-semibold">{t("audit.title", "Chronological audit")}</h1>
				<p className="text-xs text-muted-foreground">
					{t("audit.note", "Durable semantic history: every governance change and task action with its author and reason. This is not the CDC replay log.")}
				</p>
			</header>

			<div className="grid gap-4 xl:grid-cols-2">
				<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("audit.managerTimeline", "Project policy timeline")}>
					<h2 className="text-sm font-semibold">{t("audit.managerTimeline", "Project policy timeline")}</h2>
					{managerAudit.isPending ? (
						<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
					) : managerAudit.isError ? (
						<div role="alert" className="space-y-2">
							<p className="text-sm text-destructive">{apiErrorMessage(managerAudit.error)}</p>
							<Button variant="outline" onClick={() => managerAudit.refetch()}>{t("common.retry", "Retry")}</Button>
						</div>
					) : managerRows.length === 0 ? (
						<p className="text-sm text-muted-foreground">{t("audit.noManagerHistory", "No Manager governance actions recorded yet.")}</p>
					) : (
						<>
							<AuditList rows={managerRows} label={t("audit.managerTimeline", "Project policy timeline")} />
							<LoadMore
								hasNextPage={managerAudit.hasNextPage}
								isFetching={managerAudit.isFetchingNextPage}
								onClick={() => managerAudit.fetchNextPage()}
								labels={{ more: t("common.loadMore", "Load more"), loading: t("common.loadingMore", "Loading…") }}
							/>
						</>
					)}
				</section>

				<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("audit.taskTimeline", "Task audit")}>
					<div className="flex flex-wrap items-end gap-2">
						<label className="grid flex-1 gap-1.5 text-sm">
							<span className="text-muted-foreground">{t("audit.pickTask", "Task")}</span>
							<select
								className={fieldClass}
								value={taskFilter}
								onChange={(event) => setTaskFilter(event.target.value)}
								aria-label={t("audit.pickTask", "Task")}
							>
								<option value="">{t("audit.pickTaskHint", "Select a task…")}</option>
								{taskOptions.map((task) => (
									<option key={task.task.id} value={task.task.id}>
										{task.revision.definition.title} ({task.task.id})
									</option>
								))}
							</select>
						</label>
						<Button variant="outline" disabled={taskFilter === ""} onClick={() => navigate({ to: "/projects/$projectId/tasks", params: { projectId } })}>
							{t("audit.openGraph", "Open in graph")}
						</Button>
					</div>
					{tasks.isError ? (
						<p role="alert" className="text-sm text-destructive">{apiErrorMessage(tasks.error)}</p>
					) : null}
					{taskFilter === "" ? (
						<p className="text-sm text-muted-foreground">{t("audit.taskHint", "Pick a task to read its durable planning and ownership history.")}</p>
					) : taskAudit.isPending ? (
						<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
					) : taskAudit.isError ? (
						<div role="alert" className="space-y-2">
							<p className="text-sm text-destructive">{apiErrorMessage(taskAudit.error)}</p>
							<Button variant="outline" onClick={() => taskAudit.refetch()}>{t("common.retry", "Retry")}</Button>
						</div>
					) : taskRows.length === 0 ? (
						<p className="text-sm text-muted-foreground">{t("audit.noTaskHistory", "No audit entries recorded for this task.")}</p>
					) : (
						<>
							<AuditList rows={taskRows} label={t("audit.taskTimeline", "Task audit")} />
							<LoadMore
								hasNextPage={taskAudit.hasNextPage}
								isFetching={taskAudit.isFetchingNextPage}
								onClick={() => taskAudit.fetchNextPage()}
								labels={{ more: t("common.loadMore", "Load more"), loading: t("common.loadingMore", "Loading…") }}
							/>
						</>
					)}
				</section>
			</div>
		</main>
	);
}
