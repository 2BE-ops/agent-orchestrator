import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiErrorMessage } from "../lib/api-client";
import { createProviderBinding, listProviderBindings, registryProjectsQuery, registryQueryRoot, updateProviderBinding, type RegistryDefinition } from "../lib/registry-api";
import { agentModelsQueryOptions } from "../hooks/useAgentModelsQuery";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

type Definition = NonNullable<RegistryDefinition["agentType"]>;
const selectClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm";

export function NativeProviderBindings({ definition, onChange }: { definition: Definition; onChange: (patch: Partial<Definition>) => void }) {
	const { t } = useTranslation();
	const client = useQueryClient();
	const [creating, setCreating] = useState(false);
	const [name, setName] = useState("");
	const [provider, setProvider] = useState("");
	const [projectId, setProjectId] = useState("");
	const [reason, setReason] = useState("");
	const bindings = useInfiniteQuery({ queryKey: [...registryQueryRoot, "bindings"], queryFn: ({ pageParam }) => listProviderBindings(pageParam), initialPageParam: "", getNextPageParam: (page) => page.nextCursor || undefined });
	const projects = useQuery({ ...registryProjectsQuery, enabled: creating });
	const catalog = useQuery({ ...agentModelsQueryOptions(definition.harness, projectId), enabled: creating });
	const all = bindings.data?.pages.flatMap((page) => page.items) ?? [];
	const selected = all.find((item) => item.id === definition.providerBindingId);
	const providers = [...new Set((catalog.data?.models ?? []).map((model) => model.provider).filter((value): value is string => !!value))];
	const create = useMutation({ mutationFn: () => createProviderBinding({ name, harness: definition.harness, provider, projectId, reason }), onSuccess: (binding) => {
		onChange({ providerBindingId: binding.id, providerBindingRequired: false, config: { ...definition.config, model: "", mode: "", effort: "" } });
		setCreating(false); setName(""); setReason("");
		void client.invalidateQueries({ queryKey: registryQueryRoot });
	} });
	const toggle = useMutation({ mutationFn: () => updateProviderBinding(selected!.id, { name: selected!.name, enabled: !selected!.enabled, expectedRevision: selected!.revision, reason: "User changed provider binding availability" }), onSuccess: () => void client.invalidateQueries({ queryKey: registryQueryRoot }) });
	return <fieldset className="space-y-3 rounded border border-border p-3">
		<legend className="px-1 text-sm font-medium">{t("registry.providerBinding", "Provider binding")}</legend>
		<p className="text-sm text-muted-foreground">{t("registry.bindingHelp", "Reuse a provider already configured in the native harness. Authentication stays with the harness; bindings do not create independent accounts.")}</p>
		<label className="grid gap-1.5 text-sm">{t("registry.connection", "Local provider reference")}<select aria-label={t("registry.connection", "Local provider reference")} className={selectClass} value={definition.providerBindingId ?? ""} onChange={(event) => onChange({ providerBindingId: event.target.value, providerBindingRequired: false, config: { ...definition.config, model: "", effort: "", mode: "" } })}>
			<option value="">{definition.providerBindingRequired ? t("registry.selectBinding", "Select a local binding before launch") : t("registry.nativeDefault", "Native configuration defaults")}</option>
			{definition.providerBindingId && !selected && <option value={definition.providerBindingId}>{t("registry.bindingNotLoaded", "Selected binding is unavailable or not loaded")}</option>}
			{all.filter((item) => item.harness === definition.harness).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.provider || t("registry.nativeDefault", "Native configuration defaults")}{!item.enabled ? ` · ${t("registry.disabled", "Disabled")}` : ""}</option>)}
		</select></label>
		{selected?.projectId && <p className="text-xs text-muted-foreground">{t("registry.bindingProject", "Restricted to project")}: {selected.projectId}</p>}
		{bindings.isError && <p role="alert">{apiErrorMessage(bindings.error)}</p>}
		{bindings.hasNextPage && <Button type="button" variant="outline" onClick={() => void bindings.fetchNextPage()} disabled={bindings.isFetchingNextPage}>{t("registry.loadMore", "Load more")}</Button>}
		<div className="flex flex-wrap gap-2"><Button type="button" variant="outline" onClick={() => setCreating(!creating)}>{t("registry.newBinding", "Create provider binding")}</Button>{selected && <Button type="button" variant="outline" disabled={toggle.isPending} onClick={() => toggle.mutate()}>{selected.enabled ? t("registry.disableBinding", "Disable binding") : t("registry.enableBinding", "Enable binding")}</Button>}</div>
		{toggle.isError && <p role="alert">{apiErrorMessage(toggle.error)}</p>}
		{creating && <div className="space-y-3 border-t border-border pt-3">
			<label className="grid gap-1.5 text-sm">{t("registry.bindingName", "Binding name")}<Input maxLength={120} value={name} onChange={(event) => setName(event.target.value)} /></label>
			<label className="grid gap-1.5 text-sm">{t("registry.bindingScope", "Native configuration scope")}<select aria-label={t("registry.bindingScope", "Native configuration scope")} className={selectClass} value={projectId} onChange={(event) => { setProjectId(event.target.value); setProvider(""); }}><option value="">{t("registry.allProjects", "All projects (native defaults)")}</option>{projects.data?.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
			<label className="grid gap-1.5 text-sm">{t("registry.configuredProvider", "Configured native provider")}<select aria-label={t("registry.configuredProvider", "Configured native provider")} className={selectClass} value={provider} onChange={(event) => setProvider(event.target.value)}><option value="">{t("registry.nativeDefault", "Native configuration defaults")}</option>{providers.map((id) => <option key={id} value={id}>{id}</option>)}</select></label>
			{catalog.isError && <p role="alert">{apiErrorMessage(catalog.error)}</p>}{catalog.data?.warning && <p className="text-sm text-warning">{catalog.data.warning}</p>}
			<label className="grid gap-1.5 text-sm">{t("registry.bindingReason", "Reason for this binding")}<Input maxLength={2000} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
			{create.isError && <p role="alert">{apiErrorMessage(create.error)}</p>}
			<Button type="button" disabled={create.isPending || !name.trim() || !reason.trim()} onClick={() => create.mutate()}>{t("registry.saveBinding", "Save binding")}</Button>
		</div>}
	</fieldset>;
}
