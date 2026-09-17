import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { MigrationPopup } from "../components/MigrationPopup";
import { SessionsBoard } from "../components/SessionsBoard";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { hasCompletedOnboarding } from "../lib/onboarding-finish";
import { useUiStore } from "../stores/ui-store";

export const Route = createFileRoute("/_shell/")({
	component: ShellIndex,
});

function ShellIndex() {
	const navigate = useNavigate();
	const workspaceQuery = useWorkspaceQuery();
	const settingsModal = useUiStore((state) => state.settingsModal);

	useEffect(() => {
		if (!settingsModal && !hasCompletedOnboarding()) {
			void navigate({ to: "/onboarding", replace: true });
		}
	}, [navigate, settingsModal]);

	useEffect(() => {
		if (!workspaceQuery.isSuccess) return;
		const workspaces = workspaceQuery.data ?? [];
		if (workspaces.length !== 1) return;
		const [workspace] = workspaces;
		if (workspace.id !== "scratch" || workspace.kind !== "scratch") return;
		void navigate({
			to: "/projects/$projectId",
			params: { projectId: "scratch" },
			replace: true,
		});
	}, [navigate, workspaceQuery.data, workspaceQuery.isSuccess]);
	return (
		<>
			<MigrationPopup />
			<SessionsBoard />
		</>
	);
}
