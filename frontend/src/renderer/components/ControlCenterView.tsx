import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	adaptiveTasksQueryRoot,
	cancelProjectWork,
	controlCenterQueryRoot,
	getProjectControl,
	listProjectExperiments,
	listProjectNeedsHuman,
	listProjectRecommendations,
	resolveTaskNeedsHuman,
	setProjectControl,
} from "../lib/adaptive-api";
import { apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

const fieldClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary";

// The stored state machine from migration 0182: these are the deterministic
// transitions the transition trigger accepts; everything else is refused.
const allowedTransitions: Record<string, string[]> = {
	running: ["paused", "draining", "stopped"],
	paused: ["running", "stopped"],
	draining: ["running", "paused", "stopped"],
	stopped: ["running"],
};

function stateTone(state: string): string {
	switch (state) {
		case "running":
			return "border-emerald-500/50 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
		case "paused":
		case "draining":
			return "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400";
		case "stopped":
			return "border-destructive/50 bg-destructive/10 text-destructive";
		default:
			return "border-border bg-muted text-muted-foreground";
	}
}

function formatWhen(value: string | null | undefined): string {
	if (!value) return "—";
	const parsed = new Date(value);
	return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

type ControlTarget = "running" | "paused" | "draining" | "stopped";

function ControlStateCard({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [reason, setReason] = useState("");
	const [mutationError, setMutationError] = useState("");

	const control = useQuery({
		queryKey: [...controlCenterQueryRoot, projectId, "control"],
		queryFn: () => getProjectControl(projectId),
	});
	const setMutation = useMutation({
		mutationFn: (target: ControlTarget) => setProjectControl(projectId, { state: target, reason }),
		onSuccess: () => {
			setMutationError("");
			setReason("");
			void queryClient.invalidateQueries({ queryKey: controlCenterQueryRoot });
			void queryClient.invalidateQueries({ queryKey: adaptiveTasksQueryRoot });
		},
		onError: (error: Error) => setMutationError(apiErrorMessage(error)),
	});

	if (control.isPending) {
		return (
			<section className="rounded-lg border border-border p-4" aria-label={t("control.state", "Project control")}>
				<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
			</section>
		);
	}
	if (control.isError) {
		return (
			<section className="rounded-lg border border-border p-4" aria-label={t("control.state", "Project control")}>
				<div role="alert" className="space-y-2">
					<p className="text-sm text-destructive">{apiErrorMessage(control.error)}</p>
					<Button variant="outline" onClick={() => control.refetch()}>{t("common.retry", "Retry")}</Button>
				</div>
			</section>
		);
	}

	const view = control.data.view;
	const effective = view.effectiveState;
	const allowed = allowedTransitions[effective] ?? [];
	const targets: ControlTarget[] = ["running", "paused", "draining", "stopped"];
	const labels: Record<ControlTarget, string> = {
		running: t("control.resume", "Resume"),
		paused: t("control.pause", "Pause"),
		draining: t("control.drain", "Drain"),
		stopped: t("control.stop", "Stop"),
	};
	const requestTarget = (target: ControlTarget) => {
		if (target === "stopped" && !window.confirm(t("control.stopConfirm", "Stop the project? Admissions stay fenced until resumed."))) return;
		setMutation.mutate(target);
	};

	return (
		<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("control.state", "Project control")}>
			<header className="flex flex-wrap items-center gap-2">
				<h2 className="text-sm font-semibold">{t("control.state", "Project control")}</h2>
				<Badge className={cn("border", stateTone(effective))}>{effective}</Badge>
				{effective !== view.control.state ? (
					<span className="text-xs text-muted-foreground">
						{t("control.storedAs", "stored {{state}} (drain derives from live leases)", { state: view.control.state })}
					</span>
				) : null}
				<span className="ml-auto text-xs text-muted-foreground">
					{t("control.activeAttempts", "{{total}} active attempts", { total: view.activeAttempts })}
				</span>
			</header>
			<dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
				<dt className="text-muted-foreground">{t("control.lastChange", "Last change")}</dt>
				<dd>{formatWhen(view.control.updatedAt)}</dd>
				<dt className="text-muted-foreground">{t("control.actor", "Actor")}</dt>
				<dd>{view.control.actor.kind}:{view.control.actor.id}</dd>
				{view.control.reason ? (
					<>
						<dt className="text-muted-foreground">{t("control.reasonCol", "Reason")}</dt>
						<dd>{view.control.reason}</dd>
					</>
				) : null}
			</dl>
			<div className="flex flex-wrap items-end gap-2" role="group" aria-label={t("control.transitions", "Deterministic controls")}>
				<label className="grid gap-1.5 text-sm">
					<span className="text-muted-foreground">{t("control.reason", "Reason (optional)")}</span>
					<Input value={reason} onChange={(event) => setReason(event.target.value)} />
				</label>
				{targets.map((target) => (
					<Button
						key={target}
						variant="outline"
						className={target === "stopped" ? "text-destructive" : undefined}
						disabled={!allowed.includes(target) || setMutation.isPending}
						onClick={() => requestTarget(target)}
					>
						{labels[target]}
					</Button>
				))}
			</div>
			<p className="text-xs text-muted-foreground">
				{t("control.fenceNote", "Fences apply inside the same transactions: paused/draining/stopped refuse new workers and reservations; dispatch replays stay valid.")}
			</p>
			{mutationError ? <p role="alert" className="text-sm text-destructive">{mutationError}</p> : null}
		</section>
	);
}

function CancelWorkCard({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [scope, setScope] = useState<"pending" | "all">("pending");
	const [reason, setReason] = useState("");
	const [mutationError, setMutationError] = useState("");

	const cancelMutation = useMutation({
		mutationFn: () => cancelProjectWork(projectId, { scope, reason }),
		onSuccess: () => {
			setMutationError("");
			void queryClient.invalidateQueries({ queryKey: controlCenterQueryRoot });
			void queryClient.invalidateQueries({ queryKey: adaptiveTasksQueryRoot });
		},
		onError: (error: Error) => setMutationError(apiErrorMessage(error)),
	});
	const result = cancelMutation.data?.cancel;

	const request = () => {
		if (scope === "all" && !window.confirm(t("control.cancelAllConfirm", "Cancel all work? Live leased workers are asked to stop."))) return;
		cancelMutation.mutate();
	};

	return (
		<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("control.cancelWork", "Cancel work")}>
			<h2 className="text-sm font-semibold">{t("control.cancelWork", "Cancel work")}</h2>
			<div className="flex flex-wrap items-end gap-2">
				<label className="grid gap-1.5 text-sm">
					<span className="text-muted-foreground">{t("control.scope", "Scope")}</span>
					<select
						className={fieldClass}
						value={scope}
						onChange={(event) => setScope(event.target.value as "pending" | "all")}
						aria-label={t("control.scope", "Scope")}
					>
						<option value="pending">{t("control.scopePending", "Pending (unleased work only)")}</option>
						<option value="all">{t("control.scopeAll", "All (also mark leased work cancelling)")}</option>
					</select>
				</label>
				<label className="grid gap-1.5 text-sm">
					<span className="text-muted-foreground">{t("control.reason", "Reason (optional)")}</span>
					<Input value={reason} onChange={(event) => setReason(event.target.value)} />
				</label>
				<Button variant="outline" className="text-destructive" disabled={cancelMutation.isPending} onClick={request}>
					{cancelMutation.isPending ? t("control.cancelling", "Cancelling…") : t("control.cancel", "Cancel work")}
				</Button>
			</div>
			{mutationError ? <p role="alert" className="text-sm text-destructive">{mutationError}</p> : null}
			{result ? (
				<div className="space-y-2 text-sm" role="status">
					<p>
						{t("control.cancelledCount", "Cancelled {{total}} tasks.", { total: result.result.cancelled.length })}
						{result.result.retained.length > 0
							? " " + t("control.retainedCount", "{{total}} live leases were retained and reported.", { total: result.result.retained.length })
							: ""}
					</p>
					{(result.terminations ?? []).length > 0 ? (
						<ul className="space-y-1" aria-label={t("control.terminations", "Requested terminations")}>
							{(result.terminations ?? []).map((termination) => (
								<li key={termination.sessionId} className={termination.error ? "text-destructive" : "text-muted-foreground"}>
									{termination.sessionId}
									{termination.error ? ` — ${termination.error}` : ` — ${t("control.terminationAccepted", "stop requested")}`}
								</li>
							))}
						</ul>
					) : null}
					{!result.killServiceWired ? (
						<p className="text-xs text-muted-foreground">{t("control.noKillService", "No kill service wired in this daemon; terminations were not requested.")}</p>
					) : null}
				</div>
			) : null}
		</section>
	);
}

function NeedsHumanCard({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const [resolutions, setResolutions] = useState<Record<string, string>>({});
	const [mutationError, setMutationError] = useState("");

	const pages = useInfiniteQuery({
		queryKey: [...controlCenterQueryRoot, projectId, "needs-human"],
		queryFn: ({ pageParam }) => listProjectNeedsHuman(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextAfterId || undefined,
	});
	const items = pages.data?.pages.flatMap((page) => page.items) ?? [];
	const resolveMutation = useMutation({
		mutationFn: (item: { taskId: string; resolution: string }) => resolveTaskNeedsHuman(item.taskId, { resolution: item.resolution }),
		onSuccess: () => {
			setMutationError("");
			void queryClient.invalidateQueries({ queryKey: controlCenterQueryRoot });
			void queryClient.invalidateQueries({ queryKey: adaptiveTasksQueryRoot });
		},
		onError: (error: Error) => setMutationError(apiErrorMessage(error)),
	});

	return (
		<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("control.needsHuman", "Needs Human")}>
			<h2 className="text-sm font-semibold">{t("control.needsHuman", "Needs Human")}</h2>
			{pages.isPending ? (
				<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
			) : pages.isError ? (
				<div role="alert" className="space-y-2">
					<p className="text-sm text-destructive">{apiErrorMessage(pages.error)}</p>
					<Button variant="outline" onClick={() => pages.refetch()}>{t("common.retry", "Retry")}</Button>
				</div>
			) : items.length === 0 ? (
				<p className="text-sm text-muted-foreground">{t("control.noNeedsHuman", "No open requests for human input.")}</p>
			) : (
				<>
					<ul className="space-y-2" aria-label={t("control.needsHumanList", "Needs Human items")}>
						{items.map((item) => (
							<li key={item.id} className="space-y-2 rounded-md border border-border p-3 text-sm">
								<div className="flex flex-wrap items-center gap-2">
									<Badge className="border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400">{item.reasonCode}</Badge>
									<button
										type="button"
										className="font-medium underline-offset-2 hover:underline"
										onClick={() => navigate({ to: "/projects/$projectId/tasks", params: { projectId } })}
									>
										{item.taskId}
									</button>
									<span className="text-xs text-muted-foreground">{formatWhen(item.createdAt)}</span>
								</div>
								<p className="text-muted-foreground">{item.detail}</p>
								{item.resolution ? (
									<p className="text-xs text-muted-foreground">
										{t("control.resolvedAs", "Resolved as {{resolution}} by {{actor}}", { resolution: item.resolution.resolution, actor: item.resolution.actor.kind })}
									</p>
								) : (
									<div className="flex flex-wrap items-end gap-2">
										<label className="grid flex-1 gap-1.5 text-sm">
											<span className="text-muted-foreground">{t("control.resolution", "Resolution")}</span>
											<Input
												value={resolutions[item.id] ?? ""}
												onChange={(event) => setResolutions((current) => ({ ...current, [item.id]: event.target.value }))}
											/>
										</label>
										<Button
											variant="outline"
											disabled={(resolutions[item.id] ?? "").trim() === "" || resolveMutation.isPending}
											onClick={() => resolveMutation.mutate({ taskId: item.taskId, resolution: resolutions[item.id] })}
										>
											{t("control.resolve", "Resolve")}
										</Button>
									</div>
								)}
							</li>
						))}
					</ul>
					{pages.hasNextPage ? (
						<Button variant="outline" disabled={pages.isFetchingNextPage} onClick={() => pages.fetchNextPage()}>
							{pages.isFetchingNextPage ? t("common.loadingMore", "Loading…") : t("common.loadMore", "Load more")}
						</Button>
					) : null}
				</>
			)}
			{mutationError ? <p role="alert" className="text-sm text-destructive">{mutationError}</p> : null}
		</section>
	);
}

function EvolutionCard({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const experiments = useInfiniteQuery({
		queryKey: [...controlCenterQueryRoot, projectId, "experiments"],
		queryFn: ({ pageParam }) => listProjectExperiments(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextAfterId || undefined,
	});
	const recommendations = useInfiniteQuery({
		queryKey: [...controlCenterQueryRoot, projectId, "recommendations"],
		queryFn: ({ pageParam }) => listProjectRecommendations(projectId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextAfterId || undefined,
	});
	const experimentItems = experiments.data?.pages.flatMap((page) => page.items) ?? [];
	const recommendationItems = recommendations.data?.pages.flatMap((page) => page.items) ?? [];

	return (
		<div className="grid gap-4 xl:grid-cols-2">
			<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("control.experiments", "Experiments")}>
				<h2 className="text-sm font-semibold">{t("control.experiments", "Experiments")}</h2>
				{experiments.isPending ? (
					<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
				) : experiments.isError ? (
					<div role="alert" className="space-y-2">
						<p className="text-sm text-destructive">{apiErrorMessage(experiments.error)}</p>
						<Button variant="outline" onClick={() => experiments.refetch()}>{t("common.retry", "Retry")}</Button>
					</div>
				) : experimentItems.length === 0 ? (
					<p className="text-sm text-muted-foreground">{t("control.noExperiments", "No sealed experiments recorded.")}</p>
				) : (
					<>
						<ul className="space-y-2" aria-label={t("control.experimentList", "Experiment list")}>
							{experimentItems.map((experiment) => (
								<li key={experiment.id} className="space-y-1 rounded-md border border-border p-3 text-sm">
									<div className="flex flex-wrap items-center gap-2">
										<Badge className={cn("border", experiment.status === "concluded" ? "border-border bg-muted text-muted-foreground" : "border-primary/60 bg-primary/10 text-primary")}>
											{experiment.status}
										</Badge>
										<Badge className="border-border bg-muted text-muted-foreground">{experiment.kind}</Badge>
										<span className="text-xs text-muted-foreground">{experiment.entryId}</span>
									</div>
									<p>{experiment.hypothesis}</p>
									<p className="text-xs text-muted-foreground">
										{t("control.cohorts", "control v{{control}} vs candidate v{{candidate}} · minimum {{minimum}} samples", {
											control: experiment.controlVersion,
											candidate: experiment.candidateVersion,
											minimum: experiment.minimumSamples,
										})}
									</p>
									{experiment.conclusion ? (
										<p className="text-xs text-muted-foreground">
											{t("control.conclusion", "Concluded {{outcome}} ({{evidence}} comparable samples)", {
												outcome: experiment.conclusion.outcome,
												evidence: experiment.conclusion.evidence.control.comparableAttempts + experiment.conclusion.evidence.candidate.comparableAttempts,
											})}
										</p>
									) : null}
								</li>
							))}
						</ul>
						{experiments.hasNextPage ? (
							<Button variant="outline" disabled={experiments.isFetchingNextPage} onClick={() => experiments.fetchNextPage()}>
								{experiments.isFetchingNextPage ? t("common.loadingMore", "Loading…") : t("common.loadMore", "Load more")}
							</Button>
						) : null}
					</>
				)}
			</section>

			<section className="space-y-3 rounded-lg border border-border p-4" aria-label={t("control.recommendations", "Recommendations")}>
				<h2 className="text-sm font-semibold">{t("control.recommendations", "Recommendations")}</h2>
				{recommendations.isPending ? (
					<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
				) : recommendations.isError ? (
					<div role="alert" className="space-y-2">
						<p className="text-sm text-destructive">{apiErrorMessage(recommendations.error)}</p>
						<Button variant="outline" onClick={() => recommendations.refetch()}>{t("common.retry", "Retry")}</Button>
					</div>
				) : recommendationItems.length === 0 ? (
					<p className="text-sm text-muted-foreground">{t("control.noRecommendations", "No sealed recommendations recorded.")}</p>
				) : (
					<>
						<ul className="space-y-2" aria-label={t("control.recommendationList", "Recommendation list")}>
							{recommendationItems.map((recommendation) => (
								<li key={recommendation.id} className="space-y-1 rounded-md border border-border p-3 text-sm">
									<div className="flex flex-wrap items-center gap-2">
										<Badge className={cn("border", recommendation.status === "pending" ? "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400" : "border-border bg-muted text-muted-foreground")}>
											{recommendation.status}
										</Badge>
										<Badge className="border-border bg-muted text-muted-foreground">{recommendation.kind}</Badge>
										<span className="text-xs text-muted-foreground">{recommendation.entryId}</span>
									</div>
									<p>{recommendation.observation}</p>
									<p className="text-xs text-muted-foreground">
										{t("control.fromVersion", "from v{{version}} · {{total}} samples", { version: recommendation.fromVersion, total: recommendation.sampleSize })}
									</p>
									{recommendation.decision ? (
										<p className="text-xs text-muted-foreground">
											{t("control.decided", "Decided {{disposition}}", { disposition: recommendation.decision.disposition })}
										</p>
									) : null}
								</li>
							))}
						</ul>
						{recommendations.hasNextPage ? (
							<Button variant="outline" disabled={recommendations.isFetchingNextPage} onClick={() => recommendations.fetchNextPage()}>
								{recommendations.isFetchingNextPage ? t("common.loadingMore", "Loading…") : t("common.loadMore", "Load more")}
							</Button>
						) : null}
					</>
				)}
			</section>
		</div>
	);
}

export function ControlCenterView({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	return (
		<main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("control.title", "Project control center")}>
			<header className="mb-4">
				<h1 className="text-lg font-semibold">{t("control.title", "Project control center")}</h1>
			</header>
			<div className="grid gap-4 xl:grid-cols-2">
				<ControlStateCard projectId={projectId} />
				<CancelWorkCard projectId={projectId} />
			</div>
			<div className="mt-4 grid gap-4">
				<NeedsHumanCard projectId={projectId} />
				<EvolutionCard projectId={projectId} />
			</div>
		</main>
	);
}
