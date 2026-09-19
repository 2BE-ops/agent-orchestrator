import { createFileRoute } from "@tanstack/react-router";
import { AgentManagerDashboard } from "../components/AgentManagerDashboard";

export const Route = createFileRoute("/_shell/projects/$projectId_/manager")({
	component: ProjectManagerRoute,
});

function ProjectManagerRoute() {
	const { projectId } = Route.useParams();
	return <AgentManagerDashboard projectId={projectId} />;
}
