import { FolderOpen, GitFork } from "lucide-react";
import { useCallback, useRef, useState, type ReactNode } from "react";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { aoBridge } from "../lib/bridge";
import {
	CreateProjectFlow,
	type PreparedProjectInput,
} from "./CreateProjectFlow";

type ProjectMode = "folder" | "git";

export function OnboardingProjectSetup({
	mode,
	onModeChange,
	onPrepared,
	preparedProject,
}: {
	mode: ProjectMode;
	onModeChange: (mode: ProjectMode) => void;
	onPrepared: (input: PreparedProjectInput | null) => void;
	preparedProject: PreparedProjectInput | null;
}) {
	const [triggerNonce, setTriggerNonce] = useState(0);
	const [folderError, setFolderError] = useState<string | null>(null);
	const [isSelectingFolder, setIsSelectingFolder] = useState(false);
	const lastPreparedPath = useRef<string | null>(preparedProject?.path ?? null);

	const resetPrepared = useCallback(() => {
		lastPreparedPath.current = null;
		onPrepared(null);
	}, [onPrepared]);

	const startImport = useCallback(
		async (next: ProjectMode) => {
			resetPrepared();
			setFolderError(null);
			onModeChange(next);
			if (next === "folder") {
				setIsSelectingFolder(true);
				try {
					const path = await aoBridge.app.chooseDirectory("Choose a project repository");
					if (!path) return;
					const { data, error } = await apiClient.POST("/api/v1/imports/validate", {
						body: { importKind: "project", path },
					});
					if (error || !data) throw new Error(apiErrorMessage(error, "Could not validate this folder."));
					if (!data.isValid || data.nextStep === "error" || data.nextStep === "choose_import_kind") {
						throw new Error("This folder cannot be used as a project yet.");
					}
					let defaultBranch: string | undefined;
					try {
						defaultBranch = (await aoBridge.app.getRepositoryBranch(path)) ?? undefined;
					} catch {
						defaultBranch = undefined;
					}
					onPrepared({
						path,
						defaultBranch,
						repositorySetup: !data.root.isRepo
							? "NOT_A_GIT_REPO"
							: !data.root.hasCommit
								? "PROJECT_UNBORN"
								: null,
					});
				} catch (error) {
					setFolderError(error instanceof Error ? error.message : "Could not prepare this folder.");
				} finally {
					setIsSelectingFolder(false);
				}
				return;
			}
			setTriggerNonce((nonce) => nonce + 1);
		},
		[onModeChange, onPrepared, resetPrepared],
	);

	return (
		<>
			<div className="flex w-full max-w-[520px] flex-col gap-3 self-start">
				<ProjectSourceButton
					disabled={isSelectingFolder}
					icon={<GitFork aria-hidden="true" />}
					label="Clone from Git"
					onClick={() => void startImport("git")}
				/>
				<ProjectSourceButton
					disabled={isSelectingFolder}
					icon={<FolderOpen aria-hidden="true" />}
					label="Open local folder"
					onClick={() => void startImport("folder")}
				/>
				{folderError ? <p className="col-span-full text-center text-xs text-destructive">{folderError}</p> : null}
			</div>
			<CreateProjectFlow
				mode="choose"
				variant="onboarding"
				onCloneProject={async () => undefined}
				onCreateProject={async () => undefined}
				onInitializeProject={async (path) => {
					const { error } = await apiClient.POST("/api/v1/projects/initialize", { body: { path } });
					if (error) throw new Error(apiErrorMessage(error));
				}}
				onboardingTrigger={{
					kind: mode === "folder" ? "folder" : "clone",
					nonce: triggerNonce,
				}}
				prepareOnly={{
					onPrepared: (input) => {
						lastPreparedPath.current = input.path;
						onPrepared(input);
					},
				}}
			/>
		</>
	);
}

function ProjectSourceButton({ disabled, icon, label, onClick }: { disabled?: boolean; icon: ReactNode; label: string; onClick: () => void }) {
	return (
		<button
			type="button"
			onClick={onClick}
			disabled={disabled}
			aria-label={label}
			className="flex w-full items-center gap-3 rounded-lg bg-card px-4 py-3 text-left hover:bg-muted hover:text-foreground active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:pointer-events-none disabled:opacity-50"
		>
			<span className="grid size-8 shrink-0 place-items-center text-muted-foreground [&_svg]:size-4">{icon}</span>
			<span className="min-w-0 text-sm font-medium leading-5 text-foreground">{label}</span>
		</button>
	);
}
