import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "./api-client";

// Read and control surfaces for the stage-21/22 desktop views: the task graph,
// the Agent Manager dashboard, performance metrics, project knowledge, the
// chronological audit trails and the project control center consume the durable
// adaptive APIs (stages 10-20) through the generated typed client.

export const adaptiveTasksQueryRoot = ["adaptive-tasks"] as const;
export const managerDashboardQueryRoot = ["agent-manager-dashboard"] as const;
export const performanceQueryRoot = ["adaptive-performance"] as const;
export const knowledgeQueryRoot = ["project-knowledge"] as const;
export const adaptiveAuditQueryRoot = ["adaptive-audit"] as const;
export const controlCenterQueryRoot = ["project-control-center"] as const;

export type AdaptiveTaskView = components["schemas"]["AdaptiveTaskView"];
export type AdaptiveTaskState = components["schemas"]["AdaptiveTaskState"];
export type TaskAttemptView = components["schemas"]["TaskAttemptView"];
export type RoutingOutcome = components["schemas"]["DomainManagerRoutingOutcome"];
export type ManagerControllerState = components["schemas"]["AgentManagerCurrentControllerResponse"];
export type ManagerConfiguration = components["schemas"]["AgentManagerConfiguration"];
export type ProjectSessionView = components["schemas"]["ControllersSessionView"];
export type TaskPerformancePage = components["schemas"]["TaskPerformancePage"];
export type TaskPerformanceAttempt = components["schemas"]["TaskPerformanceAttempt"];
export type TaskPerformanceSummary = components["schemas"]["TaskPerformanceSummary"];
export type PerformanceGroupBy = TaskPerformanceSummary["groupBy"];
export type ManagerRoutingSummary = components["schemas"]["DomainManagerRoutingSummary"];
export type OrchestratorPlanningSummary = components["schemas"]["DomainOrchestratorPlanningSummary"];
export type KnowledgeEntry = components["schemas"]["KnowledgeView"];
export type KnowledgeVersion = components["schemas"]["KnowledgeVersion"];
export type KnowledgeDefinition = components["schemas"]["KnowledgeDefinition"];
export type ProjectControlView = components["schemas"]["DomainProjectControlView"];
export type TaskNeedsHumanItem = components["schemas"]["DomainTaskNeedsHuman"];
export type AgentManagerAuditEntry = components["schemas"]["AgentManagerAudit"];
export type TaskAuditEntry = components["schemas"]["TaskAudit"];
export type EvolutionExperimentItem = components["schemas"]["DomainEvolutionExperiment"];
export type EvolutionRecommendationItem = components["schemas"]["DomainEvolutionRecommendation"];

/** Every phase the read projection can derive, in board order. */
export const taskPhases = [
	"planned",
	"blocked",
	"ready",
	"leased",
	"working",
	"failed",
	"completed",
	"needs-human",
	"cancelling",
	"cancelled",
] as const;

export type TaskPhase = AdaptiveTaskState["phase"] | "all";

export async function listProjectTasks(projectId: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/tasks", {
		params: { path: { id: projectId }, query: { cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getProjectTask(taskId: string) {
	const result = await apiClient.GET("/api/v1/tasks/{taskId}", {
		params: { path: { taskId } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listTaskAttempts(taskId: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/tasks/{taskId}/attempts", {
		params: { path: { taskId }, query: { cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getManagerController(projectId: string) {
	const result = await apiClient.GET("/api/v1/projects/{id}/agent-manager/controller", {
		params: { path: { id: projectId } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listManagerConfigurations(projectId: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/agent-manager/configurations", {
		params: { path: { id: projectId }, query: { cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listRoutingOutcomes(projectId: string, after = 0) {
	const result = await apiClient.GET("/api/v1/projects/{id}/agent-manager/routing-outcomes", {
		params: { path: { id: projectId }, query: { after, limit: 20 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

/**
 * Active sessions for one project. The population view filters worker-kind
 * rows client-side: the sessions list API is shared board infrastructure and
 * has no kind filter.
 */
export async function listProjectActiveSessions(projectId: string) {
	const result = await apiClient.GET("/api/v1/sessions", {
		params: { query: { project: projectId, active: true } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!.sessions;
}

export function workerSessionsOf(sessions: ProjectSessionView[]): ProjectSessionView[] {
	return sessions.filter((session) => session.kind === "worker");
}

/**
 * The attribution and performance windows are complete bounded cohorts (the
 * daemon refuses windows over 366 days), so the default picker window is the
 * trailing 30 days in RFC3339 with the local clock.
 */
export function defaultMetricsWindow(): { from: string; to: string } {
	const to = new Date();
	const from = new Date(to.getTime() - 30 * 24 * 60 * 60 * 1000);
	return { from: from.toISOString(), to: to.toISOString() };
}

export async function listTaskPerformance(projectId: string, from: string, to: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/task-performance", {
		params: { path: { id: projectId }, query: { from, to, cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getTaskPerformanceSummary(
	projectId: string,
	from: string,
	to: string,
	groupBy?: components["schemas"]["TaskPerformanceSummary"]["groupBy"],
) {
	const result = await apiClient.GET("/api/v1/projects/{id}/task-performance/summary", {
		params: { path: { id: projectId }, query: { from, to, groupBy } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getManagerRoutingSummary(projectId: string, from: string, to: string) {
	const result = await apiClient.GET("/api/v1/projects/{id}/agent-manager/routing-summary", {
		params: { path: { id: projectId }, query: { from, to } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getOrchestratorPlanningSummary(projectId: string, from: string, to: string) {
	const result = await apiClient.GET("/api/v1/projects/{id}/orchestrator/planning-summary", {
		params: { path: { id: projectId }, query: { from, to } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export type KnowledgeFilter = {
	cursor?: string;
	status?: components["schemas"]["KnowledgeDefinition"]["status"];
	kind?: components["schemas"]["KnowledgeDefinition"]["kind"];
	search?: string;
};

export async function listProjectKnowledge(projectId: string, filter: KnowledgeFilter = {}) {
	const result = await apiClient.GET("/api/v1/projects/{id}/knowledge", {
		params: { path: { id: projectId }, query: { ...filter, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getProjectKnowledge(knowledgeId: string) {
	const result = await apiClient.GET("/api/v1/knowledge/{knowledgeId}", {
		params: { path: { knowledgeId } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listKnowledgeVersions(knowledgeId: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/knowledge/{knowledgeId}/versions", {
		params: { path: { knowledgeId }, query: { cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function createProjectKnowledge(
	projectId: string,
	body: components["schemas"]["KnowledgeCreateRequest"],
) {
	const result = await apiClient.POST("/api/v1/projects/{id}/knowledge", {
		params: { path: { id: projectId } },
		body,
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function reviseProjectKnowledge(
	knowledgeId: string,
	body: components["schemas"]["KnowledgeReviseRequest"],
) {
	const result = await apiClient.POST("/api/v1/knowledge/{knowledgeId}/versions", {
		params: { path: { knowledgeId } },
		body,
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getProjectControl(projectId: string) {
	const result = await apiClient.GET("/api/v1/projects/{id}/control", {
		params: { path: { id: projectId } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function setProjectControl(
	projectId: string,
	body: components["schemas"]["ControlStateInput"],
) {
	const result = await apiClient.POST("/api/v1/projects/{id}/control", {
		params: { path: { id: projectId } },
		body,
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function cancelProjectWork(
	projectId: string,
	body: components["schemas"]["ControlCancelInput"],
) {
	const result = await apiClient.POST("/api/v1/projects/{id}/control/cancel-work", {
		params: { path: { id: projectId } },
		body,
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listProjectNeedsHuman(projectId: string, afterId = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/needs-human", {
		params: { path: { id: projectId }, query: { afterId, limit: 20 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function resolveTaskNeedsHuman(
	taskId: string,
	body: components["schemas"]["ControlResolveInput"],
) {
	const result = await apiClient.POST("/api/v1/tasks/{taskId}/needs-human/resolve", {
		params: { path: { taskId } },
		body,
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listManagerAudit(projectId: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/agent-manager/audit", {
		params: { path: { id: projectId }, query: { cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listTaskAudit(taskId: string, cursor = "") {
	const result = await apiClient.GET("/api/v1/tasks/{taskId}/audit", {
		params: { path: { taskId }, query: { cursor, limit: 100 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listProjectExperiments(projectId: string, afterId = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/experiments", {
		params: { path: { id: projectId }, query: { afterId, limit: 20 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function listProjectRecommendations(projectId: string, afterId = "") {
	const result = await apiClient.GET("/api/v1/projects/{id}/recommendations", {
		params: { path: { id: projectId }, query: { afterId, limit: 20 } },
	});
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}
