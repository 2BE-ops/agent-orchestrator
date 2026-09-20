import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import {
	getRegistryVersion,
	listRegistry,
	registryQueryRoot,
	registryVersions,
	type WorkerSelection,
} from "../lib/registry-api";
import { agentModelsQueryOptions } from "../hooks/useAgentModelsQuery";
import { AgentModelCombobox } from "./settings/AgentModelCombobox";
import { Button } from "./ui/button";

const selectClass =
	"w-full rounded border border-border bg-background px-3 py-2 text-sm";

export function WorkerSelectionPanel({
	selection,
	onChange,
	projectId,
	disabled,
}: {
	selection?: WorkerSelection;
	onChange: (next: WorkerSelection | undefined) => void;
	projectId: string;
	disabled?: boolean;
}) {
	const { t } = useTranslation();
	const types = useInfiniteQuery({
		queryKey: [...registryQueryRoot, "agent_type", "list"],
		queryFn: ({ pageParam }) => listRegistry("agent_type", pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	// `?? []` per page: a success-shaped response missing `items`/`versions`
	// (e.g. a mocked or otherwise malformed payload) must degrade to an empty
	// list, not crash the whole New Task dialog on `item.entry` below.
	const entries =
		types.data?.pages.flatMap((page) => page.items ?? []) ?? [];
	const selected = entries.find(
		(item) => item.entry.id === selection?.agentTypeId,
	);
	const versions = useInfiniteQuery({
		queryKey: [...registryQueryRoot, selection?.agentTypeId, "launch-versions"],
		enabled: !!selection,
		queryFn: ({ pageParam }) =>
			registryVersions("agent_type", selection!.agentTypeId, pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const versionEntries =
		versions.data?.pages.flatMap((page) => page.versions ?? []) ?? [];
	const version = useQuery({
		queryKey: [
			...registryQueryRoot,
			selection?.agentTypeId,
			selection?.version,
			"launch",
		],
		enabled: !!selection,
		queryFn: () =>
			getRegistryVersion(
				"agent_type",
				selection!.agentTypeId,
				selection!.version!,
			),
	});
	const definition = version.data?.definition.agentType;
	const harness = selection?.overrides.harness ?? definition?.harness ?? "";
	const native = useQuery({
		queryKey: ["agent-configuration", harness, "tui"],
		enabled: !!harness,
		queryFn: async () => {
			const result = await apiClient.GET(
				"/api/v1/agents/{agent}/configuration",
				{ params: { path: { agent: harness }, query: { mode: "tui" } } },
			);
			if (result.error) throw new Error(apiErrorMessage(result.error));
			return result.data!;
		},
	});
	const models = useQuery({
		...agentModelsQueryOptions(harness, projectId),
		enabled: !!harness,
	});
	const skills = useInfiniteQuery({
		queryKey: [...registryQueryRoot, "skill", "list"],
		enabled: !!selection,
		queryFn: ({ pageParam }) => listRegistry("skill", pageParam),
		initialPageParam: "",
		getNextPageParam: (page) => page.nextCursor || undefined,
	});
	const override = (patch: Partial<WorkerSelection["overrides"]>) => {
		if (selection)
			onChange({
				...selection,
				overrides: { ...selection.overrides, ...patch },
			});
	};
	const hasModel = native.data?.fields.some((field) => field.key === "model");
	const usesModes = models.data?.selectionMode === "mode";
	const permissions = native.data?.fields.find(
		(field) => field.key === "permissions",
	);
	return (
		<fieldset
			disabled={disabled}
			className="space-y-3 rounded-lg border border-border p-3"
		>
			<label className="grid gap-1.5 text-sm">
				{t("registry.workerConfiguration", "Worker configuration")}
				<select
					aria-label={t("registry.workerConfiguration", "Worker configuration")}
					className={selectClass}
					value={selection?.agentTypeId ?? ""}
					onChange={(event) => {
						const type = entries.find(
							(item) => item.entry.id === event.target.value,
						);
						onChange(
							type
								? {
										agentTypeId: type.entry.id,
										version: type.entry.activeVersion,
										overrides: {},
									}
								: undefined,
						);
					}}
				>
					<option value="">
						{t(
							"registry.existingDefaults",
							"Project or manual harness settings",
						)}
					</option>
					{selection && !selected && (
						<option value={selection.agentTypeId}>
							{t("registry.selectedType", "Selected Agent Type")}
						</option>
					)}
					{entries.map((item) => (
						<option
							key={item.entry.id}
							value={item.entry.id}
							disabled={!item.entry.metadata.enabled}
						>
							{item.entry.metadata.name} · v{item.entry.activeVersion}
							{!item.entry.metadata.enabled
								? ` · ${t("registry.disabled", "Disabled")}`
								: ""}
						</option>
					))}
				</select>
			</label>
			{types.isError && <p role="alert">{apiErrorMessage(types.error)}</p>}
			{types.hasNextPage && (
				<Button
					type="button"
					variant="outline"
					onClick={() => void types.fetchNextPage()}
				>
					{t("registry.loadMore", "Load more")}
				</Button>
			)}
			{selection && (
				<>
					<label className="grid gap-1.5 text-sm">
						{t("registry.pinnedVersion", "Pinned Agent Type version")}
						<select
							aria-label={t(
								"registry.pinnedVersion",
								"Pinned Agent Type version",
							)}
							className={selectClass}
							value={selection.version}
							onChange={(event) =>
								onChange({
									...selection,
									version: Number(event.target.value),
									overrides: {},
								})
							}
						>
							{!versionEntries.some(
								(item) => item.number === selection.version,
							) && (
								<option value={selection.version}>v{selection.version}</option>
							)}
							{versionEntries.map((item) => (
								<option key={item.number} value={item.number}>
									v{item.number} · {item.reason}
								</option>
							))}
						</select>
					</label>
					{versions.hasNextPage && (
						<Button
							type="button"
							variant="outline"
							onClick={() => void versions.fetchNextPage()}
						>
							{t("registry.moreVersions", "More versions")}
						</Button>
					)}
					{versions.isError && (
						<p role="alert">{apiErrorMessage(versions.error)}</p>
					)}
					{version.isPending && (
						<p role="status">{t("registry.loading", "Loading registry…")}</p>
					)}
					{version.isError && (
						<p role="alert">{apiErrorMessage(version.error)}</p>
					)}
					{definition && (
						<>
							<p className="text-sm text-muted-foreground">
								{harness} ·{" "}
								{definition.config.model ||
									t("registry.projectDefault", "Project default")}{" "}
								· {definition.skills.length} {t("registry.skills", "Skills")}
							</p>
							<details className="space-y-3 text-sm">
								<summary className="cursor-pointer">
									{t("registry.oneOffOverrides", "One-off overrides")}
								</summary>
								<p className="text-muted-foreground">
									{t(
										"registry.overrideHelp",
										"These choices apply only to this worker. The Agent Type will not change.",
									)}
								</p>
								<label className="grid gap-1.5">
									{t("registry.sessionMode", "Session interface")}
									<select
										aria-label={t("registry.sessionMode", "Session interface")}
										className={selectClass}
										value={selection.overrides.sessionMode ?? ""}
										onChange={(event) =>
											override({ sessionMode: event.target.value || undefined })
										}
									>
										<option value="">
											{t(
												"registry.inheritType",
												"Inherit Agent Type and project defaults",
											)}
										</option>
										{native.data?.sessionModes.map((mode) => (
											<option key={mode} value={mode}>
												{mode === "chat"
													? t("registry.chat", "Chat")
													: t("registry.terminal", "Native terminal")}
											</option>
										))}
									</select>
								</label>
								{hasModel && !usesModes && (
									<>
										<label className="flex items-center gap-2">
											<input
												type="checkbox"
												checked={selection.overrides.model !== undefined}
												onChange={(event) =>
													override({
														model: event.target.checked
															? (definition.config.model ?? "")
															: undefined,
														mode: undefined,
														effort: undefined,
													})
												}
											/>
											{t("registry.overrideModel", "Override model")}
										</label>
										{selection.overrides.model !== undefined && (
											<AgentModelCombobox
												aria-label={t(
													"registry.workerModel",
													"Worker model override",
												)}
												value={selection.overrides.model ?? ""}
												models={models.data?.models ?? []}
												customModelEntry={
													models.data?.customModelEntry ?? "none"
												}
												agentLabel={harness}
												disabled={!models.data || models.isFetching}
												onChange={(model) => override({ model, effort: "" })}
												onCustom={(model) => override({ model, effort: "" })}
												tuning={
													models.data?.models.some(
														(item) => item.efforts?.length,
													)
														? {
																effort: selection.overrides.effort ?? "",
																onEffortChange: (effort) =>
																	override({ effort }),
																roleLabel: t("registry.worker", "Worker"),
															}
														: undefined
												}
											/>
										)}
									</>
								)}
								{usesModes && (
									<label className="grid gap-1.5">
										{t("registry.agentMode", "Agent mode")}
										<select
											aria-label={t("registry.agentMode", "Agent mode")}
											className={selectClass}
											value={selection.overrides.mode ?? ""}
											onChange={(event) =>
												override({
													mode: event.target.value || undefined,
													model: undefined,
													effort: undefined,
												})
											}
										>
											<option value="">
												{t(
													"registry.inheritType",
													"Inherit Agent Type and project defaults",
												)}
											</option>
											{models.data?.models.map((mode) => (
												<option key={mode.id} value={mode.id}>
													{mode.label}
												</option>
											))}
										</select>
									</label>
								)}
								{permissions && (
									<label className="grid gap-1.5">
										{t("registry.permissions", "Permissions")}
										<select
											aria-label={t("registry.permissions", "Permissions")}
											className={selectClass}
											value={selection.overrides.permissions ?? ""}
											onChange={(event) =>
												override({
													permissions: event.target.value || undefined,
												})
											}
										>
											<option value="">
												{t(
													"registry.inheritType",
													"Inherit Agent Type and project defaults",
												)}
											</option>
											{permissions.options.map((permission) => (
												<option key={permission} value={permission}>
													{permission}
												</option>
											))}
										</select>
									</label>
								)}
								<label className="flex items-center gap-2">
									<input
										type="checkbox"
										checked={selection.overrides.instructions !== undefined}
										onChange={(event) =>
											override({
												instructions: event.target.checked
													? definition.instructions
													: undefined,
											})
										}
									/>
									{t("registry.overrideInstructions", "Override instructions")}
								</label>
								{selection.overrides.instructions !== undefined && (
									<textarea
										aria-label={t(
											"registry.workerInstructions",
											"Worker instructions",
										)}
										className={selectClass}
										rows={4}
										maxLength={65536}
										value={selection.overrides.instructions ?? ""}
										onChange={(event) =>
											override({ instructions: event.target.value })
										}
									/>
								)}
								<label className="flex items-center gap-2">
									<input
										type="checkbox"
										checked={selection.overrides.skills !== undefined}
										onChange={(event) =>
											override({
												skills: event.target.checked
													? [...definition.skills]
													: undefined,
											})
										}
									/>
									{t(
										"registry.overrideSkills",
										"Change Skills for this worker",
									)}
								</label>
								{selection.overrides.skills !== undefined && (
									<ol className="list-inside list-decimal">
										{selection.overrides.skills?.map((pin) => (
											<li key={pin.id}>
												{skills.data?.pages
													.flatMap((page) => page.items)
													.find((skill) => skill.entry.id === pin.id)?.entry
													.metadata.name ?? pin.id}{" "}
												· v{pin.version}{" "}
												<Button
													type="button"
													variant="ghost"
													aria-label={t(
														"registry.removeWorkerSkill",
														"Remove Skill {{id}}",
														{ id: pin.id },
													)}
													onClick={() =>
														override({
															skills: selection.overrides.skills!.filter(
																(item) => item.id !== pin.id,
															),
														})
													}
												>
													{t("common.remove", "Remove")}
												</Button>
											</li>
										))}
									</ol>
								)}
								{selection.overrides.skills !== undefined && (
									<div className="space-y-2">
										{skills.data?.pages
											.flatMap((page) => page.items)
											.map((skill) => {
												const pin = selection.overrides.skills!.find(
													(item) => item.id === skill.entry.id,
												);
												return (
													<label
														key={skill.entry.id}
														className="flex items-center gap-2"
													>
														<input
															type="checkbox"
															checked={!!pin}
															disabled={!skill.entry.metadata.enabled && !pin}
															onChange={(event) =>
																override({
																	skills: event.target.checked
																		? [
																				...selection.overrides.skills!,
																				{
																					id: skill.entry.id,
																					version: skill.entry.activeVersion,
																				},
																			]
																		: selection.overrides.skills!.filter(
																				(item) => item.id !== skill.entry.id,
																			),
																})
															}
														/>
														{skill.entry.metadata.name} · v
														{pin?.version ?? skill.entry.activeVersion}
													</label>
												);
											})}
										{skills.hasNextPage && (
											<Button
												type="button"
												variant="outline"
												onClick={() => void skills.fetchNextPage()}
											>
												{t("registry.loadMore", "Load more")}
											</Button>
										)}
									</div>
								)}
								{native.isError && (
									<p role="alert">{apiErrorMessage(native.error)}</p>
								)}
								{models.isError && (
									<p role="alert">{apiErrorMessage(models.error)}</p>
								)}
								{skills.isError && (
									<p role="alert">{apiErrorMessage(skills.error)}</p>
								)}
							</details>
						</>
					)}
				</>
			)}
		</fieldset>
	);
}
