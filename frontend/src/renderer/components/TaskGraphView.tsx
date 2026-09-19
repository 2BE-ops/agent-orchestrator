import { useInfiniteQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useMemo, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import {
	adaptiveTasksQueryRoot,
	listProjectTasks,
	listTaskAttempts,
	taskPhases,
	type AdaptiveTaskView,
	type TaskPhase,
} from "../lib/adaptive-api";
import { apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

const fieldClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary";
const labelClass = "grid gap-1.5 text-sm";

type GraphViewMode = "graph" | "list";

type GraphNode = {
	task: AdaptiveTaskView;
	id: string;
	depth: number;
	x: number;
	y: number;
};

type GraphEdge = {
	from: string;
	to: string;
	kind: "parent" | "dependency";
	fromX: number;
	fromY: number;
	toX: number;
	toY: number;
};

// Deterministic layered layout: nodes with no incoming parent/dependency edge
// sit in column 0, every other node one column right of its deepest incoming
// edge. Coordinates come purely from the task rows, so the graph renders
// identically on reload and needs no DOM measurement.
const NODE_WIDTH = 224;
const NODE_HEIGHT = 64;
const COLUMN_GAP = 56;
const ROW_GAP = 16;
const CANVAS_PADDING = 12;

function layoutTaskGraph(tasks: AdaptiveTaskView[]): { nodes: GraphNode[]; edges: GraphEdge[] } {
	const byId = new Map(tasks.map((task) => [task.task.id, task]));
	const incoming = new Map<string, string[]>();
	const outgoing = new Map<string, string[]>();
	const edges: { from: string; to: string; kind: "parent" | "dependency" }[] = [];
	const addEdge = (from: string, to: string, kind: "parent" | "dependency") => {
		if (!byId.has(from) || !byId.has(to) || from === to) return;
		if (edges.some((edge) => edge.from === from && edge.to === to)) return;
		edges.push({ from, to, kind });
		const list = incoming.get(to) ?? [];
		list.push(from);
		incoming.set(to, list);
		const targets = outgoing.get(from) ?? [];
		targets.push(to);
		outgoing.set(from, targets);
	};
	for (const task of tasks) {
		const definition = task.revision.definition;
		if (definition.parentId) addEdge(definition.parentId, task.task.id, "parent");
		for (const dependency of definition.dependencies ?? []) {
			addEdge(dependency, task.task.id, "dependency");
		}
	}

	const depth = new Map<string, number>();
	const visitDepth = (id: string, seen: Set<string>): number => {
		const cached = depth.get(id);
		if (cached !== undefined) return cached;
		// The task store rejects cycles at write time; the seen set keeps a
		// partially loaded forest renderable instead of recursing forever.
		if (seen.has(id)) return 0;
		seen.add(id);
		const parents = incoming.get(id) ?? [];
		let level = 0;
		for (const parent of parents) level = Math.max(level, visitDepth(parent, seen) + 1);
		seen.delete(id);
		depth.set(id, level);
		return level;
	};
	for (const task of tasks) visitDepth(task.task.id, new Set());

	const columns = new Map<number, AdaptiveTaskView[]>();
	for (const task of tasks) {
		const level = depth.get(task.task.id) ?? 0;
		const column = columns.get(level) ?? [];
		column.push(task);
		columns.set(level, column);
	}

	const positions = new Map<string, { x: number; y: number }>();
	const nodes: GraphNode[] = [];
	for (const [level, column] of [...columns.entries()].sort((left, right) => left[0] - right[0])) {
		column.sort((left, right) => left.task.id.localeCompare(right.task.id));
		column.forEach((task, row) => {
			const position = {
				x: CANVAS_PADDING + level * (NODE_WIDTH + COLUMN_GAP),
				y: CANVAS_PADDING + row * (NODE_HEIGHT + ROW_GAP),
			};
			positions.set(task.task.id, position);
			nodes.push({ task, id: task.task.id, depth: level, ...position });
		});
	}

	const laidOut: GraphEdge[] = edges.flatMap((edge) => {
		const from = positions.get(edge.from);
		const to = positions.get(edge.to);
		if (!from || !to) return [];
		return [{
			...edge,
			fromX: from.x + NODE_WIDTH,
			fromY: from.y + NODE_HEIGHT / 2,
			toX: to.x,
			toY: to.y + NODE_HEIGHT / 2,
		}];
	});

	return { nodes, edges: laidOut };
}

function phaseTone(phase: string): string {
	switch (phase) {
		case "working":
		case "leased":
			return "border-primary/60 bg-primary/10 text-primary";
		case "completed":
			return "border-emerald-500/50 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
		case "failed":
			return "border-destructive/50 bg-destructive/10 text-destructive";
		case "needs-human":
			return "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400";
		case "cancelled":
		case "cancelling":
			return "border-border bg-muted text-muted-foreground";
		case "blocked":
			return "border-border bg-muted text-foreground";
		default:
			return "border-border bg-background text-muted-foreground";
	}
}

function formatWhen(value: string | null | undefined): string {
	if (!value) return "—";
	const parsed = new Date(value);
	return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

export function TaskGraphView({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const [view, setView] = useState<GraphViewMode>("graph");
	const [phaseFilter, setPhaseFilter] = useState<TaskPhase>("all");
	const [search, setSearch] = useState("");
	const [selected, setSelected] = useState("");

	const pages = useInfiniteQuery({
		queryKey: [...adaptiveTasksQueryRoot, projectId, "list"],
		queryFn: ({ pageParam }) => listProjectTasks(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const all = pages.data?.pages.flatMap((page) => page.items) ?? [];
	const visible = all.filter((task) =>
		(phaseFilter === "all" || task.state.phase === phaseFilter) &&
		`${task.revision.definition.title} ${task.revision.definition.brief} ${task.task.id}`
			.toLowerCase()
			.includes(search.toLowerCase())
	);
	const layout = useMemo(() => layoutTaskGraph(visible), [visible]);
	const selectedTask = all.find((task) => task.task.id === selected);

	const width = layout.nodes.reduce((max, node) => Math.max(max, node.x + NODE_WIDTH), 0) + CANVAS_PADDING;
	const height = layout.nodes.reduce((max, node) => Math.max(max, node.y + NODE_HEIGHT), 0) + CANVAS_PADDING;
	const phaseOptions: TaskPhase[] = ["all", ...taskPhases];

	return (
		<main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("tasks.graphTitle", "Task graph")}>
			<header className="mb-5 flex flex-wrap items-center justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold">{t("tasks.graphTitle", "Task graph")}</h1>
					<p className="mt-1 text-sm text-muted-foreground">
						{t("tasks.graphDescription", "Durable work intent with dependencies and live derived state. The list view carries the same facts for screen readers.")}
					</p>
				</div>
				<div className="flex gap-2" role="group" aria-label={t("tasks.viewMode", "View mode")}>
					{(["graph", "list"] as const).map((mode) => (
						<Button
							key={mode}
							variant={view === mode ? "primary" : "outline"}
							aria-pressed={view === mode}
							onClick={() => setView(mode)}
						>
							{mode === "graph" ? t("tasks.graphView", "Graph") : t("tasks.listView", "List")}
						</Button>
					))}
				</div>
			</header>

			<div className="mb-4 flex flex-wrap gap-3">
				<label className={labelClass}>
					{t("tasks.search", "Search tasks")}
					<Input value={search} onChange={(event) => setSearch(event.target.value)} />
				</label>
				<label className={labelClass}>
					{t("tasks.phase", "Phase")}
					<select
						className={fieldClass}
						value={phaseFilter}
						onChange={(event) => setPhaseFilter(event.target.value as TaskPhase)}
					>
						{phaseOptions.map((phase) => (
							<option key={phase} value={phase}>
								{phase === "all" ? t("tasks.allPhases", "All phases") : phase}
							</option>
						))}
					</select>
				</label>
			</div>

			{pages.isPending && <p role="status">{t("tasks.loading", "Loading tasks…")}</p>}
			{pages.isError && (
				<div role="alert">
					{apiErrorMessage(pages.error)}{" "}
					<Button variant="outline" onClick={() => void pages.refetch()}>
						{t("common.retry", "Retry")}
					</Button>
				</div>
			)}
			{!pages.isPending && !pages.isError && all.length === 0 && (
				<p className="py-6 text-sm text-muted-foreground">
					{t("tasks.empty", "No task intent recorded for this project yet.")}
				</p>
			)}

			<div className="grid min-h-0 gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(300px,1fr)]">
				<section aria-label={t("tasks.board", "Task board")}>
					{all.length > 0 && view === "graph" && (
						<div
							className="relative overflow-auto rounded-lg border border-border"
							style={{ maxHeight: "70vh" }}
						>
							<div className="relative" style={{ width, height }} role="group" aria-label={t("tasks.graphCanvas", "Task dependency graph. Switch to the list view for a screen-reader friendly table.")}>
								<svg
									aria-hidden="true"
									className="absolute inset-0 pointer-events-none"
									width={width}
									height={height}
								>
									<defs>
										<marker id="task-edge-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto">
											<path d="M0,0 L8,4 L0,8 z" className="fill-border" />
										</marker>
									</defs>
									{layout.edges.map((edge) => (
										<path
											key={`${edge.from}-${edge.to}-${edge.kind}`}
											d={`M ${edge.fromX} ${edge.fromY} C ${edge.fromX + COLUMN_GAP / 2} ${edge.fromY}, ${edge.toX - COLUMN_GAP / 2} ${edge.toY}, ${edge.toX - 2} ${edge.toY}`}
											className="stroke-border"
											strokeDasharray={edge.kind === "parent" ? undefined : "4 3"}
											fill="none"
											strokeWidth={1.5}
											markerEnd="url(#task-edge-arrow)"
										/>
									))}
								</svg>
								{layout.nodes.map((node) => (
									<TaskGraphNode
						key={node.id}
						node={node}
						selected={selected === node.id}
						onSelect={() => setSelected(node.id)}
						dependsOn={visible.filter((task) => (task.revision.definition.dependencies ?? []).includes(node.id)).length}
						blockedBy={(node.task.revision.definition.dependencies ?? []).length + (node.task.revision.definition.parentId ? 1 : 0)}
					/>
								))}
							</div>
						</div>
					)}
					{all.length > 0 && view === "list" && (
						<ul className="space-y-2" aria-label={t("tasks.listLabel", "Task list")}>
							{visible.map((task) => (
								<li key={task.task.id}>
									<button
						type="button"
						aria-pressed={selected === task.task.id}
						aria-current={selected === task.task.id ? "true" : undefined}
						onClick={() => setSelected(task.task.id)}
						className={cn(
							"w-full rounded-lg border p-3 text-left focus-visible:outline-2 focus-visible:outline-primary",
							selected === task.task.id ? "border-primary bg-accent" : "border-border hover:bg-muted"
						)}
									>
										<div className="flex flex-wrap items-center justify-between gap-2">
											<strong className="break-words text-sm">{task.revision.definition.title}</strong>
											<Badge className={cn("border", phaseTone(task.state.phase))}>{task.state.phase}</Badge>
										</div>
										<p className="mt-1 text-xs text-muted-foreground">
											{t("tasks.priority", "Priority")} {task.revision.definition.priority} · {t("tasks.revision", "Revision")} {task.task.revision} · {task.revision.definition.category}
										</p>
										<p className="mt-1 break-words text-xs text-muted-foreground">{task.state.reason}</p>
									</button>
								</li>
							))}
							{visible.length === 0 && (
								<li className="py-6 text-sm text-muted-foreground">
									{t("tasks.noMatches", "No tasks match the current filters.")}
								</li>
							)}
						</ul>
					)}
					{pages.hasNextPage && (
						<Button
							variant="outline"
							className="mt-3"
							disabled={pages.isFetchingNextPage}
							onClick={() => void pages.fetchNextPage()}
						>
							{t("tasks.loadMore", "Load more")}
						</Button>
					)}
				</section>

				<section aria-label={t("tasks.details", "Task details")}>
					{!selectedTask && (
						<p className="rounded-lg border border-dashed border-border p-8 text-sm text-muted-foreground">
							{t("tasks.select", "Select a task to inspect its definition, dependencies, criteria and derived state.")}
						</p>
					)}
					{selectedTask && <TaskDetail projectId={projectId} view={selectedTask} onOpenTask={setSelected} all={all} />}
				</section>
			</div>
		</main>
	);
}

function TaskGraphNode({
	node,
	selected,
	onSelect,
	dependsOn,
	blockedBy,
}: {
	node: GraphNode;
	selected: boolean;
	onSelect: () => void;
	dependsOn: number;
	blockedBy: number;
}) {
	const { t } = useTranslation();
	const definition = node.task.revision.definition;
	const activate = (event: KeyboardEvent<HTMLButtonElement>) => {
		if (event.key === "Enter" || event.key === " ") {
			event.preventDefault();
			onSelect();
		}
	};
	const label = `${definition.title}. ${t("tasks.phase", "Phase")} ${node.task.state.phase}. ${t("tasks.depth", "Depth")} ${node.depth}. ${blockedBy > 0 ? `${t("tasks.blockedByCount", "Waits on {{total}}", { total: blockedBy })}. ` : ""}${dependsOn > 0 ? `${t("tasks.dependedByCount", "Feeds {{total}}", { total: dependsOn })}.` : ""}`;
	return (
		<button
			type="button"
			aria-pressed={selected}
			onClick={onSelect}
			onKeyDown={activate}
			className={cn(
				"absolute flex flex-col justify-center gap-1 rounded-lg border p-2 text-left focus-visible:outline-2 focus-visible:outline-primary",
				selected ? "border-primary bg-accent" : "border-border bg-background hover:bg-muted"
			)}
			style={{ left: node.x, top: node.y, width: NODE_WIDTH, height: NODE_HEIGHT }}
		>
			<span className="truncate text-sm font-medium" title={definition.title}>{definition.title}</span>
			<span className="flex items-center gap-1.5">
				<Badge className={cn("border text-[10px]", phaseTone(node.task.state.phase))}>{node.task.state.phase}</Badge>
				<span className="text-[10px] text-muted-foreground">
					{t("tasks.priority", "Priority")} {definition.priority}
				</span>
			</span>
			<span className="sr-only">{label}</span>
		</button>
	);
}

function TaskDetail({
	projectId,
	view,
	all,
	onOpenTask,
}: {
	projectId: string;
	view: AdaptiveTaskView;
	all: AdaptiveTaskView[];
	onOpenTask: (taskId: string) => void;
}) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const definition = view.revision.definition;
	const attempts = useInfiniteQuery({
		queryKey: [...adaptiveTasksQueryRoot, view.task.id, "attempts"],
		queryFn: ({ pageParam }) => listTaskAttempts(view.task.id, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const attemptItems = attempts.data?.pages.flatMap((page) => page.items) ?? [];
	const dependents = all.filter((task) =>
		(task.revision.definition.dependencies ?? []).includes(view.task.id) ||
		task.revision.definition.parentId === view.task.id
	);
	const dependencies = (definition.dependencies ?? [])
		.map((id) => all.find((task) => task.task.id === id))
		.filter((task): task is AdaptiveTaskView => Boolean(task));
	const parent = definition.parentId ? all.find((task) => task.task.id === definition.parentId) : undefined;
	const lease = view.lease;
	const criteria = view.criteria?.definition.criteria ?? [];
	return (
		<div className="space-y-4 rounded-lg border border-border p-4" aria-label={t("tasks.detailFor", "Task detail")}>
			<div>
				<h2 className="break-words text-base font-semibold">{definition.title}</h2>
				<p className="mt-1 break-all text-xs text-muted-foreground">{view.task.id}</p>
				<p className="mt-2 whitespace-pre-wrap text-sm text-muted-foreground">{definition.brief}</p>
				<div className="mt-2 flex flex-wrap items-center gap-2">
					<Badge className={cn("border", phaseTone(view.state.phase))}>{view.state.phase}</Badge>
					<Badge>{t("tasks.revision", "Revision")} {view.task.revision}</Badge>
					<Badge>{t("tasks.priority", "Priority")} {definition.priority}</Badge>
					{definition.classification && <Badge>{definition.classification}</Badge>}
				</div>
				<p className="mt-2 text-sm">{view.state.reason}</p>
				{view.state.requiresReconciliation && (
					<p className="mt-1 text-sm text-warning" role="status">
						{t("tasks.needsReconciliation", "A lease is still held; this state reconciles when the lease is recovered or released.")}
					</p>
				)}
				{view.state.needsHumanTaskId && (
					<p className="mt-1 text-sm text-warning">
						{t("tasks.needsHumanOrigin", "Blocked by a request for human input on {{taskId}}", { taskId: view.state.needsHumanTaskId })}
					</p>
				)}
			</div>

			<dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
				<dt className="text-muted-foreground">{t("tasks.category", "Category")}</dt>
				<dd>{definition.category}</dd>
				<dt className="text-muted-foreground">{t("tasks.maxAttempts", "Maximum attempts")}</dt>
				<dd>{definition.maxAttempts}</dd>
				<dt className="text-muted-foreground">{t("tasks.createdBy", "Created by")}</dt>
				<dd>{view.task.createdBy.kind}{view.task.createdBy.id ? ` · ${view.task.createdBy.id}` : ""}</dd>
				<dt className="text-muted-foreground">{t("tasks.createdAt", "Created")}</dt>
				<dd>{formatWhen(view.task.createdAt)}</dd>
				<dt className="text-muted-foreground">{t("tasks.intent", "Intent")}</dt>
				<dd>{view.intent.intent} · v{view.intent.version}</dd>
				{definition.requestedWorker && (
					<>
						<dt className="text-muted-foreground">{t("tasks.requestedWorker", "Requested worker")}</dt>
						<dd className="break-all">
							{definition.requestedWorker.agentTypeId}
							{definition.requestedWorker.version ? ` · v${definition.requestedWorker.version}` : ""}
						</dd>
					</>
				)}
				{lease && (
					<>
						<dt className="text-muted-foreground">{t("tasks.leaseHeartbeat", "Lease heartbeat")}</dt>
						<dd>{formatWhen(lease.heartbeatAt)}</dd>
						<dt className="text-muted-foreground">{t("tasks.leaseExpires", "Lease expires")}</dt>
						<dd>{formatWhen(lease.expiresAt)}</dd>
						{lease.releasedAt && (
							<>
								<dt className="text-muted-foreground">{t("tasks.leaseReleased", "Lease released")}</dt>
								<dd>{formatWhen(lease.releasedAt)}{lease.releaseReason ? ` · ${lease.releaseReason}` : ""}</dd>
							</>
						)}
					</>
				)}
				{view.completion?.verified !== undefined && view.completion.taskId !== "" && (
					<>
						<dt className="text-muted-foreground">{t("tasks.completion", "Completion")}</dt>
						<dd>
							{view.completion.verified
								? t("tasks.verified", "Verified")
								: t("tasks.notVerified", "Not verified")}
							{view.completion.reason ? ` · ${view.completion.reason}` : ""}
						</dd>
					</>
				)}
			</dl>

			<div>
				<h3 className="mb-1 text-sm font-medium">{t("tasks.structure", "Structure")}</h3>
				{parent && (
					<p className="text-sm">
						<button type="button" className="underline focus-visible:outline-2 focus-visible:outline-primary" onClick={() => onOpenTask(parent.task.id)}>
							{t("tasks.parent", "Parent")}: {parent.revision.definition.title}
						</button>
					</p>
				)}
				{dependencies.length > 0 && (
					<>
						<p className="mt-1 text-xs text-muted-foreground">{t("tasks.dependsOn", "Depends on")}</p>
						<ul className="mt-1 space-y-1">
							{dependencies.map((dependency) => (
								<li key={dependency.task.id}>
									<button type="button" className="text-sm underline focus-visible:outline-2 focus-visible:outline-primary" onClick={() => onOpenTask(dependency.task.id)}>
										{dependency.revision.definition.title} · {dependency.state.phase}
									</button>
								</li>
							))}
						</ul>
					</>
				)}
				{dependents.length > 0 && (
					<>
						<p className="mt-2 text-xs text-muted-foreground">{t("tasks.feeds", "Feeds")}</p>
						<ul className="mt-1 space-y-1">
							{dependents.map((dependent) => (
								<li key={dependent.task.id}>
									<button type="button" className="text-sm underline focus-visible:outline-2 focus-visible:outline-primary" onClick={() => onOpenTask(dependent.task.id)}>
										{dependent.revision.definition.title} · {dependent.state.phase}
									</button>
								</li>
							))}
						</ul>
					</>
				)}
				{!parent && dependencies.length === 0 && dependents.length === 0 && (
					<p className="text-sm text-muted-foreground">{t("tasks.standalone", "No parent or dependencies.")}</p>
				)}
			</div>

			<div>
				<h3 className="mb-1 text-sm font-medium">
					{t("tasks.criteria", "Acceptance criteria")}
					{view.criteria && <span className="ml-1 text-xs text-muted-foreground">v{view.criteria.number}</span>}
				</h3>
				{criteria.length === 0 && <p className="text-sm text-muted-foreground">{t("tasks.noCriteria", "No frozen criteria recorded.")}</p>}
				<ul className="space-y-1 text-sm">
					{criteria.map((criterion) => (
						<li key={criterion.id} className="break-words">
							<Badge className="mr-1">{criterion.evidenceKind}</Badge>{criterion.requirement}
						</li>
					))}
				</ul>
			</div>

			<div>
				<h3 className="mb-1 text-sm font-medium">{t("tasks.attempts", "Attempts")}</h3>
				{attempts.isPending && <p role="status">{t("tasks.loading", "Loading tasks…")}</p>}
				{attempts.isError && <p role="alert">{apiErrorMessage(attempts.error)}</p>}
				{!attempts.isPending && attemptItems.length === 0 && (
					<p className="text-sm text-muted-foreground">{t("tasks.noAttempts", "No worker has claimed this task yet.")}</p>
				)}
				<ul className="space-y-1 text-sm">
					{attemptItems.map((item) => (
						<li key={item.attempt.id} className="flex flex-wrap items-center gap-2">
							<Badge>#{item.attempt.number}</Badge>
							<span className="text-muted-foreground">{formatWhen(item.attempt.createdAt)}</span>
							{item.dispatch && (
								<Button
									variant="ghost"
									className="h-auto p-0 underline"
									onClick={() =>
										void navigate({
											to: "/projects/$projectId/sessions/$sessionId",
											params: { projectId, sessionId: item.dispatch!.sessionId },
										})
									}
								>
									{t("tasks.openWorker", "Open worker session")}
								</Button>
							)}
						</li>
					))}
				</ul>
				{attempts.hasNextPage && (
					<Button variant="outline" className="mt-2" disabled={attempts.isFetchingNextPage} onClick={() => void attempts.fetchNextPage()}>
						{t("tasks.loadMore", "Load more")}
					</Button>
				)}
			</div>
		</div>
	);
}
