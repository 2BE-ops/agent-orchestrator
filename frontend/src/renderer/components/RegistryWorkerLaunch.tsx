import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { registryProjectsQuery } from "../lib/registry-api";
import { apiErrorMessage } from "../lib/api-client";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { STANDALONE_WORKSPACE_ID } from "../types/workspace";
import { TaskComposer } from "./TaskComposer";
import { Button } from "./ui/button";

export function RegistryWorkerLaunch({
	id,
	version,
	onClose,
}: {
	id: string;
	version: number;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const client = useQueryClient();
	const [projectId, setProjectId] = useState<string>(STANDALONE_WORKSPACE_ID);
	const [submitting, setSubmitting] = useState(false);
	const projects = useQuery(registryProjectsQuery);
	return (
		<section
			aria-label={t("registry.launchWorker", "Launch worker")}
			className="space-y-3 rounded border border-border p-3"
		>
			<div className="flex items-center justify-between">
				<h3 className="font-medium">
					{t("registry.launchWorker", "Launch worker")}
				</h3>
				<Button variant="ghost" disabled={submitting} onClick={onClose}>
					{t("common.close", "Close")}
				</Button>
			</div>
			<label className="grid gap-1.5 text-sm">
				{t("registry.workerProject", "Worker project")}
				<select
					aria-label={t("registry.workerProject", "Worker project")}
					disabled={submitting}
					className="rounded border border-border bg-background p-2"
					value={projectId}
					onChange={(event) => setProjectId(event.target.value)}
				>
					<option value={STANDALONE_WORKSPACE_ID}>
						{t("registry.standaloneWorker", "Standalone worker")}
					</option>
					{projects.data?.map((project) => (
						<option key={project.id} value={project.id}>
							{project.name}
						</option>
					))}
				</select>
			</label>
			{projects.isError && (
				<p role="alert">{apiErrorMessage(projects.error)}</p>
			)}
			<TaskComposer
				projectId={projectId}
				initialWorkerSelection={{ agentTypeId: id, version, overrides: {} }}
				onSubmittingChange={setSubmitting}
				onCreated={(sessionId) => {
					void client.invalidateQueries({ queryKey: workspaceQueryKey });
					if (projectId === STANDALONE_WORKSPACE_ID)
						void navigate({
							to: "/sessions/$sessionId",
							params: { sessionId },
						});
					else
						void navigate({
							to: "/projects/$projectId/sessions/$sessionId",
							params: { projectId, sessionId },
						});
					onClose();
				}}
			/>
		</section>
	);
}
