import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiErrorMessage } from "../lib/api-client";
import { exportRegistry, importRegistry, type PortableRegistryBundle, type RegistryKind } from "../lib/registry-api";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

const textAreaClass = "w-full rounded-md border border-border bg-background p-3 text-sm";

function isSkill(value: unknown): boolean {
	if (!value || typeof value !== "object") return false;
	const skill = value as Record<string, unknown>;
	return typeof skill.instructions === "string" && ["capabilities", "requiredTools", "requiredMcpServers"].every((key) => Array.isArray(skill[key]) && (skill[key] as unknown[]).every((item) => typeof item === "string")) && Array.isArray(skill.resources);
}

function canPreview(bundle: PortableRegistryBundle): boolean {
	if (typeof bundle.description !== "string") return false;
	if (bundle.kind === "skill") return isSkill(bundle.skill) && !bundle.agentType;
	const agent = bundle.agentType;
	return !!agent && !bundle.skill && typeof agent.harness === "string" && Array.isArray(agent.skills) && agent.skills.length <= 32 && agent.skills.every((skill) => skill && typeof skill.name === "string" && isSkill(skill.definition));
}

export function RegistryImport({ kind, onCancel, onImported }: {
	kind: RegistryKind;
	onCancel: () => void;
	onImported: (id: string, requirements: string[]) => void;
}) {
	const { t } = useTranslation();
	const [content, setContent] = useState("");
	const [reason, setReason] = useState("");
	const [fileError, setFileError] = useState("");
	let bundle: PortableRegistryBundle | undefined;
	let parseError = "";
	if (content) {
		try {
			const parsed = JSON.parse(content) as PortableRegistryBundle;
			if (!parsed || parsed.schemaVersion !== 1 || parsed.kind !== kind || typeof parsed.name !== "string" || !canPreview(parsed) || new TextEncoder().encode(content).length > 1048576) {
				throw new Error(t("registry.importSchema", "Choose a schema version 1 bundle matching this registry category."));
			}
			bundle = parsed;
		} catch (error) { parseError = apiErrorMessage(error); }
	}
	const mutation = useMutation({
		mutationFn: () => importRegistry(kind, { bundle: bundle!, reason }),
		onSuccess: (result) => onImported(result.root.entry.id, result.requirements),
	});
	const skills = bundle?.agentType?.skills ?? [];
	const requirements = [bundle?.skill, ...skills.map((skill) => skill.definition)].filter(Boolean);
	return <form className="max-w-3xl space-y-4 rounded-lg border border-border p-5" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}>
		<h2 className="text-lg font-semibold">{t("registry.import", "Import definitions")}</h2>
		<p className="text-sm text-muted-foreground">{t("registry.importPolicy", "Imports create new disabled definitions. Review their content, enable their Skills and configure local provider bindings before use. Agent Manager permissions start off.")}</p>
		<label className="grid gap-2 text-sm">{t("registry.bundleFile", "Bundle file (maximum 1 MiB)")}<Input type="file" accept="application/json,.json" disabled={mutation.isPending} onChange={(event) => {
			const file = event.target.files?.[0];
			setFileError("");
			if (!file) return;
			if (file.size > 1048576) { setFileError(t("registry.fileTooLarge", "Bundle exceeds 1 MiB.")); return; }
			void file.text().then(setContent).catch((error) => setFileError(apiErrorMessage(error)));
		}} /></label>
		<label className="grid gap-2 text-sm">{t("registry.bundleJSON", "Portable bundle JSON")}<textarea aria-label={t("registry.bundleJSON", "Portable bundle JSON")} className={`${textAreaClass} font-mono`} rows={10} value={content} maxLength={1048576} disabled={mutation.isPending} onChange={(event) => setContent(event.target.value)} /></label>
		{bundle && <section aria-label={t("registry.importPreview", "Import preview")} className="space-y-2 rounded bg-muted p-3 text-sm">
			<strong>{bundle.name}</strong><p>{bundle.description}</p>
			{bundle.agentType && <p>{bundle.agentType.harness} · {skills.length} {t("registry.includedSkills", "included Skills")}</p>}
			{bundle.agentType?.requiresProviderBinding && <p>{t("registry.needsBinding", "A local provider binding is required before launch.")}</p>}
			<ul>{skills.map((skill, index) => <li key={index}>{skill.name}</li>)}</ul>
			<ul>{requirements.flatMap((skill) => [...(skill?.requiredTools ?? []), ...(skill?.requiredMcpServers ?? [])]).map((requirement, index) => <li key={index}>{t("registry.requires", "Requires")}: {requirement}</li>)}</ul>
		</section>}
		{(parseError || fileError || mutation.isError) && <p role="alert" className="text-sm text-destructive">{parseError || fileError || apiErrorMessage(mutation.error)}</p>}
		<label className="grid gap-2 text-sm">{t("registry.reason", "Reason for this change")}<Input required maxLength={2000} value={reason} disabled={mutation.isPending} onChange={(event) => setReason(event.target.value)} /></label>
		<div className="flex gap-2"><Button type="submit" disabled={!bundle || !!fileError || mutation.isPending}>{t("registry.importDisabled", "Import as disabled")}</Button><Button type="button" variant="outline" disabled={mutation.isPending} onClick={onCancel}>{t("common.cancel", "Cancel")}</Button></div>
	</form>;
}

export function RegistryExport({ kind, id, version }: { kind: RegistryKind; id: string; version: number }) {
	const { t } = useTranslation();
	const mutation = useMutation({ mutationFn: () => exportRegistry(kind, id, version) });
	const content = mutation.data ? JSON.stringify(mutation.data, null, 2) : "";
	const download = () => {
		const url = URL.createObjectURL(new Blob([content], { type: "application/json" }));
		const link = document.createElement("a");
		link.href = url;
		link.download = `ao-${kind}-v${version}.json`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	};
	return <section className="space-y-2">
		<Button variant="outline" disabled={mutation.isPending} onClick={() => mutation.mutate()}>{t("registry.export", "Export active version")}</Button>
		{mutation.isError && <p role="alert">{apiErrorMessage(mutation.error)}</p>}
		{mutation.data && <><p className="text-sm text-muted-foreground">{t("registry.exportContent", "Includes authored instructions, resources and exact Skill content. Local account and provider references are excluded.")}</p><textarea readOnly aria-label={t("registry.exportedJSON", "Exported bundle JSON")} className={`${textAreaClass} font-mono`} rows={8} value={content} /><Button variant="outline" onClick={download}>{t("registry.download", "Download JSON")}</Button></>}
	</section>;
}
