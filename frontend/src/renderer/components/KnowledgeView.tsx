import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	createProjectKnowledge,
	knowledgeQueryRoot,
	listKnowledgeVersions,
	listProjectKnowledge,
	reviseProjectKnowledge,
	type KnowledgeDefinition,
	type KnowledgeEntry,
	type KnowledgeSource,
} from "../lib/adaptive-api";
import { apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

const fieldClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary";

const kindOptions: KnowledgeDefinition["kind"][] = [
	"architecture",
	"convention",
	"interface",
	"constraint",
	"pitfall",
	"failed_approach",
	"file_relationship",
	"external_behavior",
	"question",
];

const statusOptions: KnowledgeDefinition["status"][] = [
	"candidate",
	"accepted",
	"invalidated",
	"superseded",
	"deleted",
];

const confidenceOptions: KnowledgeDefinition["confidence"][] = ["low", "medium", "high"];

function statusTone(status: string): string {
	switch (status) {
		case "accepted":
			return "border-emerald-500/50 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
		case "invalidated":
		case "deleted":
			return "border-destructive/50 bg-destructive/10 text-destructive";
		case "superseded":
			return "border-border bg-muted text-muted-foreground";
		default:
			return "border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400";
	}
}

function formatWhen(value: string | null | undefined): string {
	if (!value) return "—";
	const parsed = new Date(value);
	return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

type Draft = {
	title: string;
	content: string;
	kind: KnowledgeDefinition["kind"];
	status: KnowledgeDefinition["status"];
	confidence: KnowledgeDefinition["confidence"];
	reason: string;
	sources: KnowledgeSource[];
};

const emptyDraft: Draft = {
	title: "",
	content: "",
	kind: "architecture",
	status: "candidate",
	confidence: "medium",
	reason: "",
	sources: [{ kind: "user", reference: "" }],
};

// Mirrors the daemon's provenance gate: 1 to 16 sources, each with a kind it
// recognizes and a non-empty bounded reference, and worker provenance tied to
// its exact task, attempt and session.
function sourceValid(source: KnowledgeSource): boolean {
	const knownKinds = ["user", "worker", "file", "artifact", "external"];
	if (!knownKinds.includes(source.kind)) return false;
	if (source.reference.trim() === "" || source.reference.length > 2000) return false;
	if (source.kind === "worker" && (!source.taskId || !source.attemptId || !source.sessionId)) return false;
	return true;
}

function sourcesValid(sources: KnowledgeSource[]): boolean {
	return sources.length >= 1 && sources.length <= 16 && sources.every(sourceValid);
}

function definitionOf(draft: Draft, previous: KnowledgeDefinition | undefined): KnowledgeDefinition {
	return {
		classification: previous?.classification,
		engagementId: previous?.engagementId,
		title: draft.title,
		kind: draft.kind,
		content: draft.content,
		status: draft.status,
		confidence: draft.confidence,
		pinned: previous?.pinned ?? false,
		sources: draft.sources,
		taskIds: previous?.taskIds,
		tags: previous?.tags,
		supersededBy: previous?.supersededBy,
	};
}

function KnowledgeForm({
	draft,
	setDraft,
	onSubmit,
	busy,
	submitLabel,
	heading,
}: {
	draft: Draft;
	setDraft: (next: Draft) => void;
	onSubmit: () => void;
	busy: boolean;
	submitLabel: string;
	heading: string;
}) {
	const { t } = useTranslation();
	const valid = draft.title.trim() !== "" && draft.content.trim() !== "" && draft.reason.trim() !== "" && sourcesValid(draft.sources);
	const setSource = (index: number, patch: Partial<KnowledgeSource>) => {
		setDraft({ ...draft, sources: draft.sources.map((source, i) => (i === index ? { ...source, ...patch } : source)) });
	};
	return (
		<form
			className="space-y-3 rounded-lg border border-border p-4"
			aria-label={heading}
			onSubmit={(event) => {
				event.preventDefault();
				if (valid && !busy) onSubmit();
			}}
		>
			<h3 className="text-sm font-semibold">{heading}</h3>
			<label className="grid gap-1.5 text-sm">
				<span className="text-muted-foreground">{t("knowledge.title", "Title")}</span>
				<Input value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} />
			</label>
			<label className="grid gap-1.5 text-sm">
				<span className="text-muted-foreground">{t("knowledge.content", "Content")}</span>
				<textarea
					className={cn(fieldClass, "min-h-32 font-mono")}
					value={draft.content}
					onChange={(event) => setDraft({ ...draft, content: event.target.value })}
				/>
			</label>
			<div className="grid gap-3 sm:grid-cols-3">
				<label className="grid gap-1.5 text-sm">
					<span className="text-muted-foreground">{t("knowledge.kind", "Kind")}</span>
					<select className={fieldClass} value={draft.kind} onChange={(event) => setDraft({ ...draft, kind: event.target.value as Draft["kind"] })}>
						{kindOptions.map((kind) => <option key={kind} value={kind}>{kind}</option>)}
					</select>
				</label>
				<label className="grid gap-1.5 text-sm">
					<span className="text-muted-foreground">{t("knowledge.status", "Status")}</span>
					<select className={fieldClass} value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as Draft["status"] })}>
						{statusOptions.map((status) => <option key={status} value={status}>{status}</option>)}
					</select>
				</label>
				<label className="grid gap-1.5 text-sm">
					<span className="text-muted-foreground">{t("knowledge.confidence", "Confidence")}</span>
					<select className={fieldClass} value={draft.confidence} onChange={(event) => setDraft({ ...draft, confidence: event.target.value as Draft["confidence"] })}>
						{confidenceOptions.map((confidence) => <option key={confidence} value={confidence}>{confidence}</option>)}
					</select>
				</label>
			</div>
			<label className="grid gap-1.5 text-sm">
				<span className="text-muted-foreground">{t("knowledge.reason", "Reason (recorded with the revision)")}</span>
				<Input value={draft.reason} onChange={(event) => setDraft({ ...draft, reason: event.target.value })} />
			</label>
			<fieldset className="grid gap-2 text-sm">
				<legend className="text-muted-foreground">{t("knowledge.sources", "Sources (provenance, at least one)")}</legend>
				{draft.sources.map((source, index) => (
					<div key={index} className="grid gap-2 rounded-md border border-border p-3">
						<div className="grid gap-2 sm:grid-cols-[10rem_1fr_auto]">
							<select
								className={fieldClass}
								value={source.kind}
								aria-label={t("knowledge.sourceKind", "Source {{index}} kind", { index: index + 1 })}
								onChange={(event) => setSource(index, { kind: event.target.value as KnowledgeSource["kind"] })}
							>
								{(["user", "worker", "file", "artifact", "external"] as const).map((kind) => (
									<option key={kind} value={kind}>{kind}</option>
								))}
							</select>
							<Input
								value={source.reference}
								maxLength={2000}
								aria-label={t("knowledge.sourceReference", "Source {{index}} reference", { index: index + 1 })}
								onChange={(event) => setSource(index, { reference: event.target.value })}
							/>
							<Button
								type="button"
								variant="outline"
								disabled={draft.sources.length <= 1}
								aria-label={t("knowledge.removeSource", "Remove source {{index}}", { index: index + 1 })}
								onClick={() => setDraft({ ...draft, sources: draft.sources.filter((_, i) => i !== index) })}
							>
								{t("knowledge.removeSourceButton", "Remove")}
							</Button>
						</div>
						{source.kind === "worker" ? (
							<div className="grid gap-2 sm:grid-cols-3">
								<Input
									value={source.taskId ?? ""}
									maxLength={200}
									placeholder={t("knowledge.sourceTask", "Task ID")}
									aria-label={t("knowledge.sourceTaskAria", "Source {{index}} task", { index: index + 1 })}
									onChange={(event) => setSource(index, { taskId: event.target.value })}
								/>
								<Input
									value={source.attemptId ?? ""}
									maxLength={200}
									placeholder={t("knowledge.sourceAttempt", "Attempt ID")}
									aria-label={t("knowledge.sourceAttemptAria", "Source {{index}} attempt", { index: index + 1 })}
									onChange={(event) => setSource(index, { attemptId: event.target.value })}
								/>
								<Input
									value={source.sessionId ?? ""}
									maxLength={200}
									placeholder={t("knowledge.sourceSession", "Session ID")}
									aria-label={t("knowledge.sourceSessionAria", "Source {{index}} session", { index: index + 1 })}
									onChange={(event) => setSource(index, { sessionId: event.target.value })}
								/>
							</div>
						) : null}
					</div>
				))}
				<Button
					type="button"
					variant="outline"
					disabled={draft.sources.length >= 16}
					onClick={() => setDraft({ ...draft, sources: [...draft.sources, { kind: "file", reference: "" }] })}
				>
					{t("knowledge.addSource", "Add source")}
				</Button>
			</fieldset>
			<Button type="submit" disabled={!valid || busy}>{busy ? t("knowledge.saving", "Saving…") : submitLabel}</Button>
		</form>
	);
}

function VersionHistory({ knowledgeId }: { knowledgeId: string }) {
	const { t } = useTranslation();
	const pages = useInfiniteQuery({
		queryKey: [...knowledgeQueryRoot, "versions", knowledgeId],
		queryFn: ({ pageParam }) => listKnowledgeVersions(knowledgeId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const versions = pages.data?.pages.flatMap((page) => page.items) ?? [];
	if (pages.isPending) return <p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>;
	if (pages.isError) {
		return (
			<div role="alert" className="space-y-2">
				<p className="text-sm text-destructive">{apiErrorMessage(pages.error)}</p>
				<Button variant="outline" onClick={() => pages.refetch()}>{t("common.retry", "Retry")}</Button>
			</div>
		);
	}
	return (
		<div className="space-y-2">
			<ul className="space-y-2" aria-label={t("knowledge.history", "Version history")}>
				{versions.map((version) => (
					<li key={version.number} className="rounded-md border border-border p-3 text-sm">
						<div className="flex flex-wrap items-center gap-2">
							<Badge className="border-border bg-muted text-foreground">v{version.number}</Badge>
							<Badge className={cn("border", statusTone(version.definition.status))}>{version.definition.status}</Badge>
							<span className="text-muted-foreground">{formatWhen(version.createdAt)}</span>
						</div>
						<p className="mt-1 text-muted-foreground">
							{t("knowledge.actorReason", "{{actor}} — {{reason}}", { actor: `${version.actor.kind}:${version.actor.id}`, reason: version.reason })}
						</p>
					</li>
				))}
			</ul>
			{pages.hasNextPage ? (
				<Button variant="outline" disabled={pages.isFetchingNextPage} onClick={() => pages.fetchNextPage()}>
					{pages.isFetchingNextPage ? t("common.loadingMore", "Loading…") : t("common.loadMore", "Load more")}
				</Button>
			) : null}
		</div>
	);
}

function KnowledgeDetail({ entry, onEdit }: { entry: KnowledgeEntry; onEdit: () => void }) {
	const { t } = useTranslation();
	const definition = entry.version.definition;
	return (
		<section className="space-y-4" aria-label={t("knowledge.detail", "Knowledge detail")}>
			<div className="space-y-3 rounded-lg border border-border p-4">
				<header className="flex flex-wrap items-center gap-2">
					<h2 className="text-sm font-semibold">{definition.title}</h2>
					<Badge className={cn("border", statusTone(definition.status))}>{definition.status}</Badge>
					<Badge className="border-border bg-muted text-muted-foreground">{definition.kind}</Badge>
					{definition.pinned ? <Badge className="border-primary/60 bg-primary/10 text-primary">{t("knowledge.pinned", "Pinned")}</Badge> : null}
					<span className="ml-auto text-xs text-muted-foreground">v{entry.version.number}</span>
				</header>
				<dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
					<dt className="text-muted-foreground">{t("knowledge.confidenceCol", "Confidence")}</dt>
					<dd>{definition.confidence}</dd>
					{definition.classification ? (
						<>
							<dt className="text-muted-foreground">{t("knowledge.classification", "Classification")}</dt>
							<dd>{definition.classification}{definition.engagementId ? ` · ${definition.engagementId}` : ""}</dd>
						</>
					) : null}
					{(definition.tags ?? []).length > 0 ? (
						<>
							<dt className="text-muted-foreground">{t("knowledge.tags", "Tags")}</dt>
							<dd>{(definition.tags ?? []).join(", ")}</dd>
						</>
					) : null}
					<dt className="text-muted-foreground">{t("knowledge.sources", "Sources (provenance, at least one)")}</dt>
					<dd>
						<ul className="grid gap-1">
							{definition.sources.map((source, index) => (
								<li key={index} className="font-mono text-xs">
									{source.kind}: {source.reference}
									{source.taskId ? ` · ${source.taskId}` : ""}
								</li>
							))}
						</ul>
					</dd>
					<dt className="text-muted-foreground">{t("knowledge.updated", "Updated")}</dt>
					<dd>{formatWhen(entry.knowledge.updatedAt)}</dd>
					<dt className="text-muted-foreground">{t("knowledge.contentHash", "Content hash")}</dt>
					<dd className="font-mono text-xs">{entry.version.contentHash}</dd>
				</dl>
				<p className="whitespace-pre-wrap rounded-md bg-muted p-3 text-sm">{definition.content}</p>
				<Button variant="outline" onClick={onEdit}>{t("knowledge.revise", "Revise")}</Button>
			</div>
			<div className="space-y-2">
				<h3 className="text-sm font-semibold">{t("knowledge.history", "Version history")}</h3>
				<VersionHistory knowledgeId={entry.knowledge.id} />
			</div>
		</section>
	);
}

export function KnowledgeView({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [search, setSearch] = useState("");
	const [statusFilter, setStatusFilter] = useState<"" | KnowledgeDefinition["status"]>("");
	const [selected, setSelected] = useState("");
	const [mode, setMode] = useState<"view" | "revise" | "create">("view");
	const [draft, setDraft] = useState<Draft>(emptyDraft);
	const [mutationError, setMutationError] = useState("");

	const pages = useInfiniteQuery({
		queryKey: [...knowledgeQueryRoot, projectId, "list", search, statusFilter],
		queryFn: ({ pageParam }) => listProjectKnowledge(projectId, { cursor: pageParam, search, status: statusFilter || undefined }),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const entries = pages.data?.pages.flatMap((page) => page.items) ?? [];
	const selectedEntry = entries.find((entry) => entry.knowledge.id === selected);

	const createMutation = useMutation({
		mutationFn: (input: Draft) =>
			createProjectKnowledge(projectId, { definition: definitionOf(input, undefined), reason: input.reason }),
		onSuccess: (created) => {
			setMutationError("");
			setMode("view");
			setSelected(created.knowledge.id);
			void queryClient.invalidateQueries({ queryKey: knowledgeQueryRoot });
		},
		onError: (error: Error) => setMutationError(apiErrorMessage(error)),
	});
	const reviseMutation = useMutation({
		mutationFn: (input: { draft: Draft; entry: KnowledgeEntry }) =>
			reviseProjectKnowledge(input.entry.knowledge.id, {
				definition: definitionOf(input.draft, input.entry.version.definition),
				expectedVersion: input.entry.knowledge.version,
				reason: input.draft.reason,
			}),
		onSuccess: () => {
			setMutationError("");
			setMode("view");
			void queryClient.invalidateQueries({ queryKey: knowledgeQueryRoot });
		},
		onError: (error: Error) => setMutationError(apiErrorMessage(error)),
	});

	const startRevise = (entry: KnowledgeEntry) => {
		setMutationError("");
		setMode("revise");
		setDraft({
			title: entry.version.definition.title,
			content: entry.version.definition.content,
			kind: entry.version.definition.kind,
			status: entry.version.definition.status,
			confidence: entry.version.definition.confidence,
			reason: "",
			sources: entry.version.definition.sources.map((source) => ({ ...source })),
		});
	};
	const startCreate = () => {
		setMutationError("");
		setMode("create");
		setSelected("");
		setDraft(emptyDraft);
	};

	return (
		<main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("knowledge.title", "Project knowledge")}>
			<header className="mb-4 space-y-3">
				<h1 className="text-lg font-semibold">{t("knowledge.pageTitle", "Project knowledge")}</h1>
				<div className="flex flex-wrap items-end gap-3" role="group" aria-label={t("knowledge.filters", "Knowledge filters")}>
					<label className="grid gap-1.5 text-sm">
						<span className="text-muted-foreground">{t("knowledge.search", "Search")}</span>
						<Input
							value={search}
							onChange={(event) => setSearch(event.target.value)}
							placeholder={t("knowledge.searchPlaceholder", "Title or content")}
						/>
					</label>
					<label className="grid gap-1.5 text-sm">
						<span className="text-muted-foreground">{t("knowledge.statusFilter", "Status")}</span>
						<select
							className={fieldClass}
							value={statusFilter}
							onChange={(event) => setStatusFilter(event.target.value as typeof statusFilter)}
							aria-label={t("knowledge.statusFilter", "Status")}
						>
							<option value="">{t("knowledge.allStatuses", "Default (open statuses)")}</option>
							{statusOptions.map((status) => <option key={status} value={status}>{status}</option>)}
						</select>
					</label>
					<Button onClick={startCreate} disabled={mode === "create"}>{t("knowledge.newEntry", "New knowledge")}</Button>
				</div>
			</header>

			{mutationError ? <p role="alert" className="mb-3 text-sm text-destructive">{mutationError}</p> : null}

			<div className="grid gap-4 xl:grid-cols-2">
				<section className="space-y-3" aria-label={t("knowledge.list", "Knowledge list")}>
					{pages.isPending ? (
						<p role="status" className="text-sm text-muted-foreground">{t("common.loading", "Loading…")}</p>
					) : pages.isError ? (
						<div role="alert" className="space-y-2">
							<p className="text-sm text-destructive">{apiErrorMessage(pages.error)}</p>
							<Button variant="outline" onClick={() => pages.refetch()}>{t("common.retry", "Retry")}</Button>
						</div>
					) : entries.length === 0 ? (
						<p className="text-sm text-muted-foreground">{t("knowledge.empty", "No knowledge recorded for this project yet.")}</p>
					) : (
						<>
							<ul className="space-y-2" aria-label={t("knowledge.list", "Knowledge list")}>
								{entries.map((entry) => (
									<li key={entry.knowledge.id}>
										<button
											type="button"
											className="w-full rounded-lg border border-border p-3 text-left hover:bg-muted/50 aria-pressed:bg-muted"
											aria-pressed={selected === entry.knowledge.id}
											onClick={() => {
												setSelected(entry.knowledge.id);
												setMode("view");
											}}
										>
											<div className="flex flex-wrap items-center gap-2">
												<span className="text-sm font-medium">{entry.version.definition.title}</span>
												<Badge className={cn("border", statusTone(entry.version.definition.status))}>{entry.version.definition.status}</Badge>
												<Badge className="border-border bg-muted text-muted-foreground">{entry.version.definition.kind}</Badge>
												<span className="ml-auto text-xs text-muted-foreground">v{entry.version.number}</span>
											</div>
											<p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{entry.version.definition.content}</p>
										</button>
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
				</section>

				<section aria-label={t("knowledge.detailPane", "Knowledge detail pane")}>
					{mode === "create" ? (
						<KnowledgeForm
							heading={t("knowledge.createHeading", "Record new knowledge")}
							draft={draft}
							setDraft={setDraft}
							busy={createMutation.isPending}
							submitLabel={t("knowledge.create", "Record knowledge")}
							onSubmit={() => createMutation.mutate(draft)}
						/>
					) : mode === "revise" && selectedEntry ? (
						<KnowledgeForm
							heading={t("knowledge.reviseHeading", "Revise knowledge (v{{version}})", { version: selectedEntry.knowledge.version })}
							draft={draft}
							setDraft={setDraft}
							busy={reviseMutation.isPending}
							submitLabel={t("knowledge.saveRevision", "Save revision")}
							onSubmit={() => reviseMutation.mutate({ draft, entry: selectedEntry })}
						/>
					) : selectedEntry ? (
						<KnowledgeDetail entry={selectedEntry} onEdit={() => startRevise(selectedEntry)} />
					) : (
						<p className="text-sm text-muted-foreground">{t("knowledge.selectHint", "Select a knowledge entry to inspect its provenance and history.")}</p>
					)}
				</section>
			</div>
		</main>
	);
}
