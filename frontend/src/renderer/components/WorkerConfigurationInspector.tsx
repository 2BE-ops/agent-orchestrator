import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiErrorMessage } from "../lib/api-client";
import { getWorkerConfiguration } from "../lib/registry-api";
import { Button } from "./ui/button";

export function WorkerConfigurationInspector({
	sessionId,
}: {
	sessionId: string;
}) {
	const { t } = useTranslation();
	const query = useQuery({
		queryKey: ["worker-configuration", sessionId],
		queryFn: () => getWorkerConfiguration(sessionId),
		staleTime: Infinity,
		retry: false,
	});
	if (query.isPending)
		return (
			<p role="status" className="text-xs text-muted-foreground">
				{t(
					"registry.loadingWorkerConfiguration",
					"Loading worker configuration…",
				)}
			</p>
		);
	if (query.isError)
		return (
			<div role="alert" className="text-xs">
				{apiErrorMessage(query.error)}{" "}
				<Button variant="ghost" onClick={() => void query.refetch()}>
					{t("common.retry", "Retry")}
				</Button>
			</div>
		);
	const snapshot = query.data;
	if (!snapshot) return null;
	return (
		<section
			aria-label={t("registry.launchConfiguration", "Launch configuration")}
			className="space-y-2 border-t border-border py-3 text-xs"
		>
			<h3 className="font-medium">
				{t("registry.launchConfiguration", "Launch configuration")}
			</h3>
			<p>
				{snapshot.agentType.name} · v{snapshot.agentType.version}
			</p>
			<p className="text-muted-foreground">
				{snapshot.effective.harness} · {snapshot.effective.sessionMode} ·{" "}
				{snapshot.effective.config.model ||
					t("registry.nativeDefault", "Native configuration defaults")}
			</p>
			{snapshot.provider && (
				<p>
					{snapshot.provider.name} ·{" "}
					{snapshot.provider.provider ||
						t("registry.nativeDefault", "Native configuration defaults")}
				</p>
			)}
			<ol className="list-inside list-decimal">
				{snapshot.skills.map((skill) => (
					<li key={skill.reference.id}>
						{skill.reference.name} · v{skill.reference.version}
					</li>
				))}
			</ol>
			<p>
				{snapshot.origin} · {new Date(snapshot.createdAt).toLocaleString()}
			</p>
			<details>
				<summary className="cursor-pointer">
					{t("registry.oneOffOverrides", "One-off overrides")}
				</summary>
				<pre className="overflow-auto whitespace-pre-wrap rounded bg-muted p-2">
					{JSON.stringify(snapshot.selection.overrides, null, 2)}
				</pre>
			</details>
			<details>
				<summary className="cursor-pointer">
					{t(
						"registry.retainedInstructions",
						"Retained instructions and provenance",
					)}
				</summary>
				<pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded bg-muted p-2">
					{JSON.stringify(snapshot, null, 2)}
				</pre>
			</details>
		</section>
	);
}
