import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "./api-client";

export type RegistryKind = "agent_type" | "skill";
export type RegistryView = components["schemas"]["RegistryViewResponse"];
export type RegistryVersion = components["schemas"]["RegistryVersionResponse"];
export type RegistryDefinition = components["schemas"]["RegistryDefinition"];
export type RegistryMetadata = components["schemas"]["RegistryMetadata"];
export const registryQueryRoot = ["adaptive-registry"] as const;

const resource = (kind: RegistryKind) => kind === "agent_type" ? "/api/v1/agent-types" as const : "/api/v1/skills" as const;
const detail = (kind: RegistryKind) => kind === "agent_type" ? "/api/v1/agent-types/{id}" as const : "/api/v1/skills/{id}" as const;
const versions = (kind: RegistryKind) => kind === "agent_type" ? "/api/v1/agent-types/{id}/versions" as const : "/api/v1/skills/{id}/versions" as const;

export async function listRegistry(kind: RegistryKind, cursor = "") {
	const result = await apiClient.GET(resource(kind), { params: { query: { cursor, limit: 100 } } });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function getRegistry(kind: RegistryKind, id: string) {
	const result = await apiClient.GET(detail(kind), { params: { path: { id } } });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function registryVersions(kind: RegistryKind, id: string, cursor = "") {
	const result = await apiClient.GET(versions(kind), { params: { path: { id }, query: { cursor, limit: 100 } } });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function createRegistry(kind: RegistryKind, body: components["schemas"]["RegistryCreateInput"]) {
	const result = await apiClient.POST(resource(kind), { body });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function updateRegistry(kind: RegistryKind, id: string, body: components["schemas"]["RegistryMetadataInput"]) {
	const result = await apiClient.PATCH(detail(kind), { params: { path: { id } }, body });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function appendRegistryVersion(kind: RegistryKind, id: string, body: components["schemas"]["RegistryVersionInput"]) {
	const result = await apiClient.POST(versions(kind), { params: { path: { id } }, body });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function activateRegistryVersion(kind: RegistryKind, view: RegistryView, version: number) {
	const path = kind === "agent_type" ? "/api/v1/agent-types/{id}/activate" : "/api/v1/skills/{id}/activate";
	const result = await apiClient.POST(path, { params: { path: { id: view.entry.id } }, body: {
		version, expectedRevision: view.entry.revision, reason: `User selected version ${version}`,
	} });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function cloneRegistry(kind: RegistryKind, view: RegistryView, version: number, name: string) {
	const path = kind === "agent_type" ? "/api/v1/agent-types/{id}/clone" : "/api/v1/skills/{id}/clone";
	const result = await apiClient.POST(path, { params: { path: { id: view.entry.id } }, body: { version, name, reason: "User cloned definition" } });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}

export async function registryAudit(kind: RegistryKind, id: string, cursor = "") {
	const path = kind === "agent_type" ? "/api/v1/agent-types/{id}/audit" : "/api/v1/skills/{id}/audit";
	const result = await apiClient.GET(path, { params: { path: { id }, query: { cursor, limit: 100 } } });
	if (result.error) throw new Error(apiErrorMessage(result.error));
	return result.data!;
}
