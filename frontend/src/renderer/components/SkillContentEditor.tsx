import { useTranslation } from "react-i18next";
import type { RegistryDefinition } from "../lib/registry-api";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

type Skill = NonNullable<RegistryDefinition["skill"]>;
const fieldClass = "w-full rounded-md border border-border bg-background p-3 text-sm";

export function SkillContentEditor({ skill, onChange }: { skill: Skill; onChange: (skill: Skill) => void }) {
	const { t } = useTranslation();
	return <div className="space-y-4">
		<p className="text-sm text-muted-foreground">{t("registry.requirementsHelp", "Requirements describe capabilities a worker must have. They do not install tools or grant permissions.")}</p>
		<label className="grid gap-2 text-sm">{t("registry.tools", "Required tools (one per line)")}<textarea aria-label={t("registry.tools", "Required tools (one per line)")} className={fieldClass} rows={3} value={skill.requiredTools.join("\n")} onChange={(event) => onChange({ ...skill, requiredTools: event.target.value.split("\n") })} /></label>
		<label className="grid gap-2 text-sm">{t("registry.mcp", "Required MCP servers (one per line)")}<textarea aria-label={t("registry.mcp", "Required MCP servers (one per line)")} className={fieldClass} rows={3} value={skill.requiredMcpServers.join("\n")} onChange={(event) => onChange({ ...skill, requiredMcpServers: event.target.value.split("\n") })} /></label>
		<fieldset className="space-y-4 rounded border border-border p-3"><legend className="px-1 text-sm font-medium">{t("registry.resources", "Resources")}</legend>
			<p className="text-sm text-muted-foreground">{t("registry.resourceHelp", "Up to 32 UTF-8 resources, 64 KiB each and 256 KiB in total. Paths are relative to this Skill; resources are stored without execution.")}</p>
			{skill.resources.map((resource, index) => <div key={index} className="space-y-2 border-b border-border pb-3">
				<label className="grid gap-1 text-sm">{t("registry.resourcePath", "Resource path")} {index + 1}<Input required maxLength={240} value={resource.path} onChange={(event) => onChange({ ...skill, resources: skill.resources.map((item, i) => i === index ? { ...item, path: event.target.value } : item) })} /></label>
				<label className="grid gap-1 text-sm">{t("registry.resourceContent", "Resource content")} {index + 1}<textarea aria-label={`${t("registry.resourceContent", "Resource content")} ${index + 1}`} className={`${fieldClass} font-mono`} rows={6} maxLength={65536} value={resource.content} onChange={(event) => onChange({ ...skill, resources: skill.resources.map((item, i) => i === index ? { ...item, content: event.target.value } : item) })} /></label>
				<Button type="button" variant="ghost" onClick={() => onChange({ ...skill, resources: skill.resources.filter((_, i) => i !== index) })}>{t("registry.removeResource", "Remove resource")} {index + 1}</Button>
			</div>)}
			<Button type="button" variant="outline" disabled={skill.resources.length >= 32} onClick={() => onChange({ ...skill, resources: [...skill.resources, { path: "", content: "" }] })}>{t("registry.addResource", "Add resource")}</Button>
		</fieldset>
	</div>;
}
