import { createFileRoute } from "@tanstack/react-router";
import { KnowledgeView } from "../components/KnowledgeView";

export const Route = createFileRoute("/_shell/projects/$projectId_/knowledge")({
	component: ProjectKnowledgeRoute,
});

function ProjectKnowledgeRoute() {
	const { projectId } = Route.useParams();
	return <KnowledgeView projectId={projectId} />;
}
