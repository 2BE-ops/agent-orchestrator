import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "./api-client";

// Read surfaces for the stage-21 desktop views: the task graph and the Agent
// Manager dashboard consume the durable adaptive read APIs (stages 10-18)
// through the generated typed client. Everything here is read-only.

export const adaptiveTasksQueryRoot = ["adaptive-tasks"] as const;
export const managerDashboardQueryRoot = ["agent-manager-dashboard"] as const;

export type AdaptiveTaskView = components["schemas"]["AdaptiveTaskView"];
export type AdaptiveTaskState = components["schemas"]["AdaptiveTaskState"];
export type TaskAttemptView = components["schemas"]["TaskAttemptView"];
export type RoutingOutcome = components["schemas"]["DomainManagerRoutingOutcome"];
export type ManagerControllerState = components["schemas"]["AgentManagerCurrentControllerResponse"];
export type ManagerConfiguration = components["schemas"]["AgentManagerConfiguration"];
export type ProjectSessionView = components["schemas"]["ControllersSessionView"];

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
