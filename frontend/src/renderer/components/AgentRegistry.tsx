import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import {
	activateRegistryVersion, appendRegistryVersion, cloneRegistry, createRegistry,
	getRegistry, listRegistry, registryAudit, registryQueryRoot, registryVersions, updateRegistry,
	type RegistryDefinition, type RegistryKind, type RegistryMetadata, type RegistryVersion, type RegistryView,
} from "../lib/registry-api";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Badge } from "./ui/badge";
import { RegistryExport, RegistryImport } from "./RegistryTransfer";
import { SkillContentEditor } from "./SkillContentEditor";
import { AgentTypeConfigurationEditor } from "./AgentTypeConfigurationEditor";
import { RegistryConfigurationCheck } from "./RegistryConfigurationCheck";

const fieldClass = "w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary";
const labelClass = "grid gap-1.5 text-sm";
type Editor = { mode: "create" | "metadata" | "configuration"; view?: RegistryView };

export function AgentRegistry() {
	const { t } = useTranslation();
	const client = useQueryClient();
	const [kind, setKind] = useState<RegistryKind>("agent_type");
	const [search, setSearch] = useState("");
	const [origin, setOrigin] = useState("");
	const [selected, setSelected] = useState("");
	const [editor, setEditor] = useState<Editor>();
	const [notice, setNotice] = useState("");
	const [importing, setImporting] = useState(false);
	const entries = useInfiniteQuery({ queryKey: [...registryQueryRoot, kind, "list"],
		queryFn: ({ pageParam }) => listRegistry(kind, pageParam), initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined });
	const detail = useQuery({ queryKey: [...registryQueryRoot, kind, selected], queryFn: () => getRegistry(kind, selected), enabled: selected !== "" });
	const all = entries.data?.pages.flatMap((page) => page.items) ?? [];
	const visible = all.filter(({ entry, version }) => (!origin || entry.origin === origin) &&
		`${entry.metadata.name} ${entry.metadata.description} ${version.definition.agentType?.capabilities.join(" ") ?? version.definition.skill?.capabilities.join(" ") ?? ""}`.toLowerCase().includes(search.toLowerCase()));
	const refresh = () => { void client.invalidateQueries({ queryKey: registryQueryRoot }); };
	const label = kind === "agent_type" ? t("registry.agentTypes", "Agent Types") : t("registry.skills", "Skills");
	return <main className="flex h-full min-h-0 flex-col overflow-auto p-6" aria-label={t("registry.title", "Agent registry")}>
		<header className="mb-5 flex flex-wrap items-center justify-between gap-3">
			<div><h1 className="text-xl font-semibold">{t("registry.title", "Agent registry")}</h1><p className="mt-1 text-sm text-muted-foreground">{t("registry.description", "Reusable worker configurations and composable skills. Versions preserve the configuration used for past work.")}</p></div>
			<div className="flex gap-2"><Button variant="outline" onClick={() => { setImporting(true); setEditor(undefined); }}>{t("registry.import", "Import definitions")}</Button><Button onClick={() => { setEditor({ mode: "create" }); setImporting(false); setNotice(""); }}>{kind === "agent_type" ? t("registry.createType", "Create Agent Type") : t("registry.createSkill", "Create Skill")}</Button></div>
		</header>
		<div className="mb-4 flex gap-2" role="group" aria-label={t("registry.category", "Registry category")}>
			{(["agent_type", "skill"] as const).map((value) => <Button key={value} variant={kind === value ? "primary" : "outline"} aria-pressed={kind === value} onClick={() => { setKind(value); setSelected(""); setEditor(undefined); setNotice(""); }}>{value === "agent_type" ? t("registry.agentTypes", "Agent Types") : t("registry.skills", "Skills")}</Button>)}
		</div>
		{notice && <p role="status" className="mb-3 text-sm">{notice}</p>}
		{importing ? <RegistryImport key={kind} kind={kind} onCancel={() => setImporting(false)} onImported={(id, requirements) => { setImporting(false); setSelected(id); setNotice(requirements.join(" ")); refresh(); }} /> : editor ? <RegistryEditor key={`${kind}:${editor.mode}:${editor.view?.entry.id ?? "new"}`} kind={kind} editor={editor}
			onCancel={() => setEditor(undefined)} onSaved={(id, message) => { setEditor(undefined); setSelected(id); setNotice(message); refresh(); }} /> :
		<div className="grid min-h-0 gap-5 lg:grid-cols-[minmax(240px,1fr)_minmax(0,2fr)]">
			<section aria-label={label} className="space-y-3">
				<label className={labelClass}>{t("registry.search", "Search definitions")}<Input value={search} onChange={(e) => setSearch(e.target.value)} /></label>
				<label className={labelClass}>{t("registry.origin", "Created by")}<select className={fieldClass} value={origin} onChange={(e) => setOrigin(e.target.value)}><option value="">{t("registry.allOrigins", "All origins")}</option><option value="USER">{t("registry.user", "User")}</option><option value="AGENT_MANAGER">{t("registry.manager", "Agent Manager")}</option><option value="SYSTEM">{t("registry.system", "System")}</option></select></label>
				{entries.isPending && <p role="status">{t("registry.loading", "Loading registry…")}</p>}
				{entries.isError && <div role="alert">{apiErrorMessage(entries.error)} <Button variant="outline" onClick={() => void entries.refetch()}>{t("common.retry", "Retry")}</Button></div>}
				{!entries.isPending && !entries.isError && visible.length === 0 && <p className="py-6 text-sm text-muted-foreground">{t("registry.empty", "No matching definitions. Create one to get started.")}</p>}
				<ul className="space-y-2">{visible.map((view) => <li key={view.entry.id}><button type="button" aria-pressed={selected === view.entry.id} onClick={() => setSelected(view.entry.id)} className={`w-full rounded-lg border p-3 text-left focus-visible:outline-2 focus-visible:outline-primary ${selected === view.entry.id ? "border-primary bg-accent" : "border-border hover:bg-muted"}`}>
					<div className="flex items-center justify-between gap-2"><strong className="break-words text-sm">{view.entry.metadata.name}</strong><Badge>v{view.entry.activeVersion}</Badge></div>
					<p className="mt-1 text-xs text-muted-foreground">{view.version.definition.agentType?.harness ?? t("registry.skill", "Skill")} · {view.entry.origin}</p>
					{!view.entry.metadata.enabled && <Badge className="mt-2">{t("registry.disabled", "Disabled")}</Badge>}
				</button></li>)}</ul>
				{entries.hasNextPage && <Button disabled={entries.isFetchingNextPage} variant="outline" onClick={() => void entries.fetchNextPage()}>{t("registry.loadMore", "Load more")}</Button>}
			</section>
			<section aria-label={t("registry.details", "Definition details")}>
				{!selected && <p className="rounded-lg border border-dashed border-border p-8 text-sm text-muted-foreground">{t("registry.select", "Select a definition to inspect its configuration, versions and history.")}</p>}
				{selected && detail.isPending && <p role="status">{t("registry.loading", "Loading registry…")}</p>}
				{selected && detail.isError && <div role="alert">{apiErrorMessage(detail.error)} <Button variant="outline" onClick={() => void detail.refetch()}>{t("common.retry", "Retry")}</Button></div>}
				{detail.data && selected && <RegistryDetail key={selected} kind={kind} view={detail.data} onEdit={(mode) => setEditor({ mode, view: detail.data })} onChanged={refresh} onClone={(id) => { setSelected(id); refresh(); }} />}
			</section>
		</div>}
	</main>;
}

function RegistryDetail({ kind, view, onEdit, onChanged, onClone }: { kind: RegistryKind; view: RegistryView; onEdit: (mode: "metadata" | "configuration") => void; onChanged: () => void; onClone: (id: string) => void }) {
	const { t } = useTranslation();
	const [compare, setCompare] = useState<RegistryVersion>();
	const [cloneName, setCloneName] = useState("");
	const history = useInfiniteQuery({ queryKey: [...registryQueryRoot, kind, view.entry.id, "versions"], queryFn: ({ pageParam }) => registryVersions(kind, view.entry.id, pageParam), initialPageParam: "", getNextPageParam: (page) => page.nextCursor || undefined });
	const audit = useInfiniteQuery({ queryKey: [...registryQueryRoot, kind, view.entry.id, "audit"], queryFn: ({ pageParam }) => registryAudit(kind, view.entry.id, pageParam), initialPageParam: "", getNextPageParam: (page) => page.nextCursor || undefined });
	const action = useMutation({ mutationFn: async (input: { action: "activate" | "disable" | "clone"; version?: number }) => {
		if (input.action === "activate") await activateRegistryVersion(kind, view, input.version!);
		if (input.action === "disable") await updateRegistry(kind, view.entry.id, { metadata: { ...view.entry.metadata, enabled: !view.entry.metadata.enabled }, expectedRevision: view.entry.revision, reason: "User changed enabled state" });
		if (input.action === "clone") { const created = await cloneRegistry(kind, view, view.entry.activeVersion, cloneName); onClone(created.entry.id); }
	}, onSuccess: onChanged });
	return <div className="space-y-5 rounded-lg border border-border p-5">
		<div><h2 className="text-lg font-semibold">{view.entry.metadata.name}</h2><p className="mt-1 whitespace-pre-wrap text-sm text-muted-foreground">{view.entry.metadata.description}</p><p className="mt-2 text-xs text-muted-foreground">{view.entry.origin} · v{view.entry.activeVersion} · {t("registry.revision", "Revision")} {view.entry.revision}</p></div>
		<div className="flex flex-wrap gap-2"><Button variant="outline" onClick={() => onEdit("configuration")}>{t("registry.newVersion", "New version")}</Button><Button variant="outline" onClick={() => onEdit("metadata")}>{t("registry.editPolicy", "Edit details and policy")}</Button><Button variant="outline" disabled={action.isPending} onClick={() => action.mutate({ action: "disable" })}>{view.entry.metadata.enabled ? t("registry.disable", "Disable") : t("registry.enable", "Enable")}</Button></div>
		{action.isError && <p role="alert" className="text-destructive">{apiErrorMessage(action.error)}</p>}
		<DefinitionSummary definition={view.version.definition} />
		{kind === "agent_type" && <RegistryConfigurationCheck key={`${view.entry.activeVersion}:${view.entry.revision}`} id={view.entry.id} version={view.entry.activeVersion} />}
		<RegistryExport key={view.entry.activeVersion} kind={kind} id={view.entry.id} version={view.entry.activeVersion} />
		<section><h3 className="mb-2 font-medium">{t("registry.versions", "Version history")}</h3>
			{history.isPending && <p role="status">{t("registry.loading", "Loading registry…")}</p>}{history.isError && <p role="alert">{apiErrorMessage(history.error)}</p>}
			<ul className="space-y-2">{history.data?.pages.flatMap((page) => page.versions).map((version) => <li key={version.number} className="flex flex-wrap items-center gap-2 border-b border-border py-2 text-sm"><strong>v{version.number}</strong><span className="min-w-0 flex-1 break-words text-muted-foreground">{version.reason}</span><Button variant="ghost" onClick={() => setCompare(version)}>{t("registry.compare", "Compare")}</Button><Button variant="outline" disabled={action.isPending || version.number === view.entry.activeVersion} onClick={() => action.mutate({ action: "activate", version: version.number })}>{version.number === view.entry.activeVersion ? t("registry.active", "Active") : t("registry.useVersion", "Use version")}</Button></li>)}</ul>
			{history.hasNextPage && <Button variant="outline" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{t("registry.loadMore", "Load more")}</Button>}
			{compare && <div className="mt-3 space-y-2"><h4 className="font-medium">v{view.entry.activeVersion} / v{compare.number}</h4><div className="grid gap-3 xl:grid-cols-2"><pre aria-label={t("registry.activeConfiguration", "Active configuration")} className="overflow-auto rounded bg-muted p-3 text-xs">{JSON.stringify(view.version.definition, null, 2)}</pre><pre aria-label={t("registry.comparisonConfiguration", "Comparison configuration")} className="overflow-auto rounded bg-muted p-3 text-xs">{JSON.stringify(compare.definition, null, 2)}</pre></div><Button variant="ghost" onClick={() => setCompare(undefined)}>{t("common.close", "Close")}</Button></div>}
		</section>
		<form className="flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); action.mutate({ action: "clone" }); }}><label className={`${labelClass} flex-1`}>{t("registry.cloneName", "Name for cloned definition")}<Input required maxLength={120} value={cloneName} onChange={(e) => setCloneName(e.target.value)} /></label><Button variant="outline" type="submit" disabled={action.isPending || !cloneName.trim()}>{t("registry.clone", "Clone")}</Button></form>
		<details><summary className="cursor-pointer text-sm font-medium">{t("registry.audit", "Authoring history")}</summary>{audit.isError && <p role="alert">{apiErrorMessage(audit.error)}</p>}<ol className="mt-2 space-y-2 text-sm">{audit.data?.pages.flatMap((page) => page.events).map((event) => <li key={event.sequence} className="border-l-2 border-border pl-3"><p>{event.action} · v{event.versionNumber} · {event.origin}</p><p className="text-muted-foreground">{event.reason} · {new Date(event.createdAt).toLocaleString()}</p></li>)}</ol>{audit.hasNextPage && <Button variant="outline" disabled={audit.isFetchingNextPage} onClick={() => void audit.fetchNextPage()}>{t("registry.loadMore", "Load more")}</Button>}</details>
	</div>;
}

function DefinitionSummary({ definition }: { definition: RegistryDefinition }) {
	const { t } = useTranslation();
	const config = definition.agentType;
	const content = config ?? definition.skill;
	return <section className="space-y-3 text-sm">
		{config && <dl className="grid grid-cols-2 gap-2"><dt className="text-muted-foreground">{t("registry.harness", "Harness")}</dt><dd>{config.harness}</dd><dt className="text-muted-foreground">{t("registry.model", "Model")}</dt><dd>{config.config.model || t("registry.default", "Configured default")}</dd><dt className="text-muted-foreground">{t("registry.maxParallel", "Maximum parallel workers")}</dt><dd>{config.maxParallelWorkers}</dd></dl>}
		<div className="flex flex-wrap gap-1">{content?.capabilities.map((tag) => <Badge key={tag}>{tag}</Badge>)}</div>
		<h3 className="font-medium">{t("registry.instructions", "Instructions")}</h3><p className="max-h-72 overflow-auto whitespace-pre-wrap rounded bg-muted p-3">{content?.instructions || t("registry.noInstructions", "No additional instructions")}</p>
		{config?.providerBindingRequired && !config.providerBindingId && <p>{t("registry.needsBinding", "A local provider binding is required before launch.")}</p>}
		{definition.skill && <><h3 className="font-medium">{t("registry.requirements", "Requirements")}</h3><ul>{[...definition.skill.requiredTools, ...definition.skill.requiredMcpServers].map((requirement, index) => <li key={index}>{requirement}</li>)}</ul><h3 className="font-medium">{t("registry.resources", "Resources")}</h3>{definition.skill.resources.map((resource) => <details key={resource.path}><summary className="cursor-pointer">{resource.path}</summary><pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded bg-muted p-3 text-xs">{resource.content}</pre></details>)}</>}
		{config && config.skills.length > 0 && <div><h3 className="font-medium">{t("registry.pinnedSkills", "Pinned Skills")}</h3><ul className="mt-1 space-y-1">{config.skills.map((skill) => <li key={skill.id} className="break-all">{skill.id} · v{skill.version}</li>)}</ul></div>}
	</section>;
}

function RegistryEditor({ kind, editor, onCancel, onSaved }: { kind: RegistryKind; editor: Editor; onCancel: () => void; onSaved: (id: string, message: string) => void }) {
	const { t } = useTranslation();
	const [metadata, setMetadata] = useState<RegistryMetadata>(() => editor.view?.entry.metadata ?? { name: "", description: "", enabled: true, policy: { managerCanSelect: true, managerCanModify: false, managerCanVersion: false } });
	const [definition, setDefinition] = useState<RegistryDefinition>(() => editor.view?.version.definition ?? (kind === "agent_type" ? { agentType: { harness: "", config: {}, instructions: "", capabilities: [], skills: [], maxParallelWorkers: 1 } } : { skill: { instructions: "", capabilities: [], requiredTools: [], requiredMcpServers: [], resources: [] } }));
	const [capabilities, setCapabilities] = useState(() => (definition.agentType?.capabilities ?? definition.skill?.capabilities ?? []).join(", "));
	const [reason, setReason] = useState("");
	const showMetadata = editor.mode !== "configuration";
	const showConfig = editor.mode !== "metadata";
	const harnesses = useQuery({ queryKey: ["registry-harnesses"], enabled: kind === "agent_type" && showConfig, queryFn: async () => { const response = await apiClient.GET("/api/v1/agents"); if (response.error) throw new Error(apiErrorMessage(response.error)); return response.data!.supported; } });
	const skills = useInfiniteQuery({ queryKey: [...registryQueryRoot, "skill", "list"], enabled: kind === "agent_type" && showConfig, queryFn: ({ pageParam }) => listRegistry("skill", pageParam), initialPageParam: "", getNextPageParam: (page) => page.nextCursor || undefined });
	const setAgent = (patch: Partial<NonNullable<RegistryDefinition["agentType"]>>) => setDefinition((old) => ({ agentType: { ...old.agentType!, ...patch } }));
	const setInstructions = (instructions: string) => setDefinition((old) => old.agentType ? { agentType: { ...old.agentType, instructions } } : { skill: { ...old.skill!, instructions } });
	const save = useMutation({ mutationFn: async () => {
		const tags = capabilities.split(",").map((tag) => tag.trim()).filter(Boolean);
		const next: RegistryDefinition = definition.agentType ? { agentType: { ...definition.agentType, capabilities: tags } } : { skill: { ...definition.skill!, capabilities: tags, requiredTools: definition.skill!.requiredTools.map((value) => value.trim()).filter(Boolean), requiredMcpServers: definition.skill!.requiredMcpServers.map((value) => value.trim()).filter(Boolean) } };
		if (editor.mode === "create") return (await createRegistry(kind, { metadata, definition: next, reason })).entry.id;
		const id = editor.view!.entry.id;
		const expectedRevision = editor.view!.entry.revision;
		if (editor.mode === "metadata") await updateRegistry(kind, id, { metadata, expectedRevision, reason });
		else await appendRegistryVersion(kind, id, { definition: next, expectedRevision, reason });
		return id;
	}, onSuccess: (id) => onSaved(id, editor.mode === "configuration" ? t("registry.versionSaved", "Version saved. Select Use version to activate it for future workers.") : t("registry.saved", "Definition saved.")) });
	return <form className="max-w-3xl space-y-5 rounded-lg border border-border p-5" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
		<h2 className="text-lg font-semibold">{editor.mode === "create" ? (kind === "agent_type" ? t("registry.createType", "Create Agent Type") : t("registry.createSkill", "Create Skill")) : editor.mode === "metadata" ? t("registry.editPolicy", "Edit details and policy") : t("registry.newVersion", "New version")}</h2>
		<fieldset disabled={save.isPending} className="space-y-4">
		{showMetadata && <>
			<label className={labelClass}>{t("registry.name", "Name")}<Input required maxLength={120} value={metadata.name} onChange={(e) => setMetadata({ ...metadata, name: e.target.value })} /></label>
			<label className={labelClass}>{t("registry.descriptionLabel", "Description")}<textarea aria-label={t("registry.descriptionLabel", "Description")} className={fieldClass} rows={2} maxLength={4000} value={metadata.description} onChange={(e) => setMetadata({ ...metadata, description: e.target.value })} /></label>
			<label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={metadata.enabled} onChange={(e) => setMetadata({ ...metadata, enabled: e.target.checked })} />{t("registry.enabled", "Enabled")}</label>
			<fieldset className="space-y-2 rounded border border-border p-3"><legend className="px-1 text-sm font-medium">{t("registry.managerPermissions", "Agent Manager permissions")}</legend>
				{([ ["managerCanSelect", t("registry.canSelect", "May select")], ["managerCanModify", t("registry.canModify", "May modify details")], ["managerCanVersion", t("registry.canVersion", "May create versions")] ] as const).map(([key, label]) => <label key={key} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={metadata.policy[key]} onChange={(e) => setMetadata({ ...metadata, policy: { ...metadata.policy, [key]: e.target.checked } })} />{label}</label>)}
			</fieldset>
		</>}
		{showConfig && <>
			{definition.agentType && <>
				<label className={labelClass}>{t("registry.harness", "Harness")}<select aria-label={t("registry.harness", "Harness")} required className={fieldClass} value={definition.agentType.harness} onChange={(e) => setAgent({ harness: e.target.value, config: {}, providerBindingId: undefined, sessionMode: undefined })}><option value="">{t("registry.selectHarness", "Select a harness")}</option>{harnesses.data?.map((harness) => <option key={harness.id} value={harness.id}>{harness.label}</option>)}</select></label>
				{harnesses.isPending && <p role="status">{t("registry.loadingHarnesses", "Loading supported harnesses…")}</p>}{harnesses.isError && <p role="alert">{apiErrorMessage(harnesses.error)}</p>}
				<label className={labelClass}>{t("registry.maxParallel", "Maximum parallel workers")}<Input type="number" min={1} max={1000} required value={definition.agentType.maxParallelWorkers} onChange={(e) => setAgent({ maxParallelWorkers: Number(e.target.value) })} /></label>
				{definition.agentType.harness && <AgentTypeConfigurationEditor definition={definition.agentType} onChange={setAgent} />}
			</>}
			<label className={labelClass}>{t("registry.instructions", "Instructions")}<textarea aria-label={t("registry.instructions", "Instructions")} className={fieldClass} rows={8} maxLength={65536} required={kind === "skill"} value={definition.agentType?.instructions ?? definition.skill?.instructions ?? ""} onChange={(e) => setInstructions(e.target.value)} /></label>
			<label className={labelClass}>{t("registry.capabilities", "Capabilities (comma separated)")}<Input value={capabilities} onChange={(e) => setCapabilities(e.target.value)} /></label>
			{definition.skill && <SkillContentEditor skill={definition.skill} onChange={(skill) => setDefinition({ skill })} />}
			{definition.agentType && <fieldset className="space-y-2 rounded border border-border p-3"><legend className="px-1 text-sm font-medium">{t("registry.pinnedSkills", "Pinned Skills")}</legend>
				{skills.isError && <p role="alert">{apiErrorMessage(skills.error)}</p>}
				{skills.data?.pages.flatMap((page) => page.items).map((skill) => {
					const pin = definition.agentType!.skills.find((item) => item.id === skill.entry.id);
					return <label key={skill.entry.id} className="flex items-center gap-2 text-sm"><input type="checkbox" disabled={!skill.entry.metadata.enabled && !pin} checked={!!pin} onChange={(e) => setAgent({ skills: e.target.checked ? [...definition.agentType!.skills, { id: skill.entry.id, version: skill.entry.activeVersion }] : definition.agentType!.skills.filter((item) => item.id !== skill.entry.id) })} />{skill.entry.metadata.name} · v{pin?.version ?? skill.entry.activeVersion}{!skill.entry.metadata.enabled && ` · ${t("registry.disabled", "Disabled")}`}</label>;
				})}
				{skills.hasNextPage && <Button variant="outline" type="button" disabled={skills.isFetchingNextPage} onClick={() => void skills.fetchNextPage()}>{t("registry.loadMore", "Load more")}</Button>}
				{skills.data?.pages[0]?.items.length === 0 && <p className="text-sm text-muted-foreground">{t("registry.noSkills", "Create a Skill in the Skills tab to attach it here.")}</p>}
			</fieldset>}
		</>}
		<label className={labelClass}>{t("registry.reason", "Reason for this change")}<Input required maxLength={2000} value={reason} onChange={(e) => setReason(e.target.value)} /></label>
		</fieldset>
		{save.isError && <p role="alert" className="text-sm text-destructive">{apiErrorMessage(save.error)}</p>}
		<div className="flex gap-2"><Button type="submit" disabled={save.isPending || (showConfig && kind === "agent_type" && !definition.agentType?.harness)}>{save.isPending ? t("registry.saving", "Saving…") : t("common.save", "Save")}</Button><Button variant="outline" type="button" disabled={save.isPending} onClick={onCancel}>{t("common.cancel", "Cancel")}</Button></div>
	</form>;
}
