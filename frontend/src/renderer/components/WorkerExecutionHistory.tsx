import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { components } from "../../api/schema";
import { apiErrorMessage } from "../lib/api-client";
import {
	getWorkerExecution,
	listWorkerExecutions,
	workerExecutionsQueryRoot,
} from "../lib/registry-api";
import { Button } from "./ui/button";

function NativeConfiguration({
	harness,
	sessionMode,
	config,
}: {
	harness: string | null;
	sessionMode: string | null;
	config: components["schemas"]["AgentConfig"];
}) {
	const { t } = useTranslation();
	return (
		<p className="break-words text-muted-foreground">
			{harness} · {sessionMode} ·{" "}
			{config.model ||
				config.mode ||
				t("registry.nativeDefault", "Native configuration defaults")}
			{config.effort ? ` · ${config.effort}` : ""}
			{config.permissions ? ` · ${config.permissions}` : ""}
		</p>
	);
}

function ExecutionDetails({
	sessionId,
	executionId,
}: {
	sessionId: string;
	executionId: string;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const query = useQuery({
		queryKey: ["worker-execution-detail", sessionId, executionId],
		queryFn: () => getWorkerExecution(sessionId, executionId),
		enabled: open,
		staleTime: Infinity,
		retry: false,
	});
	return (
		<details onToggle={(event) => setOpen(event.currentTarget.open)}>
			<summary className="cursor-pointer">
				{t(
					"registry.executionProvenance",
					"Change details and retained content",
				)}
			</summary>
			{open && query.isPending && (
				<p role="status">{t("common.loading", "Loading…")}</p>
			)}
			{open && query.isError && (
				<div role="alert">
					{apiErrorMessage(query.error)}{" "}
					<Button variant="ghost" onClick={() => void query.refetch()}>
						{t("common.retry", "Retry")}
					</Button>
				</div>
			)}
			{open && query.data && (
				<pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded bg-muted p-2">
					{JSON.stringify(query.data, null, 2)}
				</pre>
			)}
		</details>
	);
}

export function WorkerExecutionHistory({ sessionId }: { sessionId: string }) {
	const { t } = useTranslation();
	const query = useInfiniteQuery({
		queryKey: [...workerExecutionsQueryRoot, sessionId],
		queryFn: ({ pageParam }) => listWorkerExecutions(sessionId, pageParam),
		initialPageParam: 0,
		getNextPageParam: (page) => page.nextCursor || undefined,
		retry: false,
	});
	const latest = query.data?.pages.reduce((a, b) =>
		a.currentSequence >= b.currentSequence ? a : b,
	);
	const current = latest?.current;
	return (
		<section
			aria-label={t("registry.executionHistory", "Execution history")}
			className="space-y-2 border-t border-border py-3 text-xs"
		>
			<h3 className="font-medium">
				{t("registry.executionHistory", "Execution history")}
			</h3>
			{query.isPending && (
				<p role="status">
					{t("registry.loadingExecutions", "Loading execution history…")}
				</p>
			)}
			{query.isError && (
				<div role="alert">
					{apiErrorMessage(query.error)}{" "}
					<Button variant="ghost" onClick={() => void query.refetch()}>
						{t("common.retry", "Retry")}
					</Button>
				</div>
			)}
			{current && (
				<div
					aria-label={t("registry.activeConfiguration", "Active configuration")}
				>
					<p className="font-medium">
						{t("registry.activeConfiguration", "Active configuration")}
					</p>
					<NativeConfiguration
						harness={current.effective.harness}
						sessionMode={current.effective.sessionMode || ""}
						config={current.effective.config}
					/>
					{current.provider && <p>{current.provider.name}</p>}
				</div>
			)}
			{latest?.pendingChange && (
				<p role="alert">
					{t(
						"registry.nativeRecoveryPending",
						"A native configuration change needs recovery. Retry its native control or resume the worker before sending another turn.",
					)}
				</p>
			)}
			{query.data &&
				query.data.pages.every((page) => page.events.length === 0) && (
					<p>
						{t(
							"registry.noExecutionChanges",
							"No configuration changes since launch.",
						)}
					</p>
				)}
			<ol className="space-y-3">
				{query.data?.pages
					.flatMap((page) => page.events)
					.map((event) => (
						<li
							key={event.activation.sequence}
							className="space-y-1 border-l border-border pl-2"
						>
							<p className="font-medium">
								{event.activation.action === "rolled_back"
									? t(
											"registry.configurationRolledBack",
											"Configuration restored",
										)
									: t("registry.configurationApplied", "Configuration applied")}
								{event.activation.sequence === latest?.currentSequence
									? ` · ${t("registry.current", "Current")}`
									: ""}
							</p>
							<NativeConfiguration
								harness={event.harness}
								sessionMode={event.sessionMode}
								config={event.config}
							/>
							<p>{event.reason}</p>
							<p className="break-words text-muted-foreground">
								{event.actor.origin} · {event.actor.id} ·{" "}
								{new Date(event.activation.createdAt).toLocaleString()}
							</p>
							<ExecutionDetails
								sessionId={sessionId}
								executionId={event.activation.operationId}
							/>
						</li>
					))}
			</ol>
			{query.hasNextPage && (
				<Button
					variant="outline"
					disabled={query.isFetchingNextPage}
					onClick={() => void query.fetchNextPage()}
				>
					{t("registry.moreExecutionHistory", "Load more changes")}
				</Button>
			)}
		</section>
	);
}
