import { createFileRoute } from "@tanstack/react-router";
import { TaskGraphView } from "../components/TaskGraphView";

export const Route = createFileRoute("/_shell/projects/$projectId_/tasks")({
	component: ProjectTasksRoute,
});

function ProjectTasksRoute() {
	const { projectId } = Route.useParams();
	return <TaskGraphView projectId={projectId} />;
}
