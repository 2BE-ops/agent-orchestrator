import { createFileRoute } from "@tanstack/react-router";
import { ControlCenterView } from "../components/ControlCenterView";

export const Route = createFileRoute("/_shell/projects/$projectId_/control")({
	component: ProjectControlRoute,
});

function ProjectControlRoute() {
	const { projectId } = Route.useParams();
	return <ControlCenterView projectId={projectId} />;
}
