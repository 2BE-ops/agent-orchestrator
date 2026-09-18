import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { getProviderBinding, registryQueryRoot, type RegistryDefinition } from "../lib/registry-api";
import { agentModelsQueryKey, agentModelsQueryOptions, refreshAgentModels } from "../hooks/useAgentModelsQuery";
import { useAgentReadinessQuery } from "../hooks/useAgentReadinessQuery";
import { useUiStore } from "../stores/ui-store";
import { AgentModelCombobox } from "./settings/AgentModelCombobox";
import { Button } from "./ui/button";
import { NativeProviderBindings } from "./NativeProviderBindings";

type Definition = NonNullable<RegistryDefinition["agentType"]>;
const selectClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm";

export function AgentTypeConfigurationEditor({ definition, onChange }: { definition: Definition; onChange: (patch: Partial<Definition>) => void }) {
	const { t } = useTranslation();
	const client = useQueryClient();
	const openSettings = useUiStore((state) => state.openGlobalSettings);
	const harness = definition.harness;
	const mode = definition.sessionMode === "chat" ? "chat" : "tui";
	const configuration = useQuery({ queryKey: ["agent-configuration", harness, mode], enabled: !!harness, retry: false, queryFn: async () => {
		const result = await apiClient.GET("/api/v1/agents/{agent}/configuration", { params: { path: { agent: harness }, query: { mode } } });
		if (result.error) throw new Error(apiErrorMessage(result.error));
		return result.data!;
	} });
	const binding = useQuery({ queryKey: [...registryQueryRoot, "bindings", definition.providerBindingId], enabled: !!definition.providerBindingId, retry: false, queryFn: () => getProviderBinding(definition.providerBindingId!) });
	const projectId = binding.data?.projectId ?? "";
	const models = useQuery(agentModelsQueryOptions(harness, projectId));
	const readiness = useAgentReadinessQuery(!!harness);
	const native = readiness.data?.agents?.find((item) => item.id === harness);
	const fields = configuration.data?.fields ?? [];
	const catalog = models.data;
	const choices = (catalog?.models ?? []).filter((model) => !binding.data?.provider || model.provider === binding.data.provider);
	const permission = fields.find((field) => field.key === "permissions");
	const hasModel = fields.some((field) => field.key === "model");
	const setConfig = (patch: Partial<Definition["config"]>) => onChange({ config: { ...definition.config, ...patch } });
	return <div className="space-y-4">
		<label className="grid gap-1.5 text-sm">{t("registry.sessionMode", "Session interface")}<select aria-label={t("registry.sessionMode", "Session interface")} className={selectClass} value={definition.sessionMode || ""} onChange={(event) => onChange({ sessionMode: event.target.value })}>
			<option value="">{t("registry.projectDefault", "Project default")}</option>
			{(configuration.data?.sessionModes ?? ["tui"]).map((value) => <option key={value} value={value}>{value === "chat" ? t("registry.chat", "Chat") : t("registry.terminal", "Native terminal")}</option>)}
		</select></label>
		<div className="space-y-2 rounded border border-border p-3 text-sm"><p>{t("registry.nativeAuth", "Uses the harness's native authentication and provider configuration.")}</p><p>{t("registry.readiness", "Readiness")}: {native?.effectiveReadiness ?? t("registry.unknown", "Unknown")}</p>{native && <p className="text-muted-foreground">{native.authentication.reason}</p>}<Button type="button" variant="outline" onClick={() => openSettings("agents")}>{t("registry.manageAuth", "Manage native sign-in")}</Button></div>
		{configuration.isPending && <p role="status">{t("registry.loadingConfiguration", "Loading supported configuration…")}</p>}
		{configuration.isError && <p role="alert">{apiErrorMessage(configuration.error)}</p>}
		<NativeProviderBindings key={harness} definition={definition} onChange={onChange} />
		{binding.isError && <p role="alert">{apiErrorMessage(binding.error)}</p>}
		{configuration.data?.warning && <p role="status" className="text-sm text-warning">{configuration.data.warning}</p>}
		{catalog?.selectionMode === "mode" ? <label className="grid gap-1.5 text-sm">{t("registry.agentMode", "Agent mode")}<select aria-label={t("registry.agentMode", "Agent mode")} className={selectClass} value={definition.config.mode || ""} onChange={(event) => setConfig({ mode: event.target.value, model: "", effort: "" })}><option value="">{t("registry.default", "Configured default")}</option>{choices.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select></label> : hasModel && <div className="grid gap-1.5 text-sm"><span>{t("registry.model", "Model")}</span><AgentModelCombobox aria-label={t("registry.model", "Model")} value={definition.config.model ?? ""} models={choices} customModelEntry={binding.data?.provider ? "configured" : catalog?.customModelEntry ?? "none"} agentLabel={harness} disabled={models.isFetching || !catalog} onChange={(model) => setConfig({ model, mode: "", effort: "" })} onCustom={(model) => setConfig({ model, mode: "", effort: "" })} onRefresh={async () => { const next = await refreshAgentModels(harness, projectId); client.setQueryData(agentModelsQueryKey(harness, projectId), next); }} tuning={choices.some((item) => item.efforts?.length) || definition.config.effort ? { effort: definition.config.effort ?? "", onEffortChange: (effort) => setConfig({ effort }), roleLabel: t("registry.agentType", "Agent Type") } : undefined} /></div>}
		{models.isError && <p role="alert">{apiErrorMessage(models.error)}</p>}
		{catalog?.warning && <p className="text-sm text-warning">{catalog.warning}</p>}
		{permission && <label className="grid gap-1.5 text-sm">{t("registry.permissions", "Permissions")}<select aria-label={t("registry.permissions", "Permissions")} className={selectClass} value={definition.config.permissions || ""} onChange={(event) => setConfig({ permissions: event.target.value as Definition["config"]["permissions"] })}><option value="">{t("registry.inheritPermissions", "Project default")}</option>{permission.options.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>}
		{mode === "chat" && configuration.data?.capabilityState === "supported" && <details className="text-sm"><summary>{t("registry.nativeCapabilities", "Native capabilities")}</summary><ul>{configuration.data.chatCapabilities.map((capability) => <li key={capability}>{capability}</li>)}</ul></details>}
	</div>;
}
