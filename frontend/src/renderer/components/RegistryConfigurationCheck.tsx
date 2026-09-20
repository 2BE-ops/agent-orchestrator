import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiErrorMessage } from "../lib/api-client";
import { checkRegistryConfiguration, registryProjectsQuery } from "../lib/registry-api";
import { Button } from "./ui/button";

export function RegistryConfigurationCheck({ id, version }: { id: string; version: number }) {
	const { t } = useTranslation();
	const [projectId, setProjectId] = useState("");
	const projects = useQuery(registryProjectsQuery);
	const check = useMutation({ mutationFn: () => checkRegistryConfiguration(id, version, projectId) });
	return <section className="space-y-2 rounded border border-border p-3 text-sm">
		<h3 className="font-medium">{t("registry.checkConfiguration", "Check configuration")}</h3>
		<label className="grid gap-1.5">{t("registry.checkProject", "Validate for project")}<select aria-label={t("registry.checkProject", "Validate for project")} className="rounded border border-border bg-background p-2" value={projectId} onChange={(event) => { setProjectId(event.target.value); check.reset(); }}><option value="">{t("registry.nativeDefaults", "Native defaults (no project)")}</option>{projects.data?.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
		<Button variant="outline" disabled={check.isPending} onClick={() => check.mutate()}>{check.isPending ? t("registry.checking", "Checking…") : t("registry.checkReadiness", "Check native readiness")}</Button>
		{check.isError && <p role="alert">{apiErrorMessage(check.error)}</p>}
		{check.data && <div role="status"><p>{check.data.ready ? t("registry.configurationReady", "Configuration is ready for a fresh launch. It will be checked again when launched.") : t("registry.configurationNotReady", "Resolve these items before a fresh launch:")}</p><ul className="mt-2 list-disc space-y-1 pl-5">{check.data.issues.map((issue, index) => <li key={index}>{issue.message}</li>)}</ul></div>}
	</section>;
}
