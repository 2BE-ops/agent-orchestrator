import { createFileRoute } from "@tanstack/react-router";
import { PerformanceMetricsView } from "../components/PerformanceMetricsView";

export const Route = createFileRoute("/_shell/projects/$projectId_/performance")({
	component: ProjectPerformanceRoute,
});

function ProjectPerformanceRoute() {
	const { projectId } = Route.useParams();
	return <PerformanceMetricsView projectId={projectId} />;
}
