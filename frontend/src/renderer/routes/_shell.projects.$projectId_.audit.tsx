import { createFileRoute } from "@tanstack/react-router";
import { AuditTimelineView } from "../components/AuditTimelineView";

export const Route = createFileRoute("/_shell/projects/$projectId_/audit")({
	component: ProjectAuditRoute,
});

function ProjectAuditRoute() {
	const { projectId } = Route.useParams();
	return <AuditTimelineView projectId={projectId} />;
}
