import { useCallback } from "react";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { aoBridge } from "../lib/bridge";
import type { CloudCpSession } from "../lib/cloud-cp";
import { createRendererCloudCpClient } from "./useCloudCp";
import { settingsQueryKey, type Settings } from "./useSettings";
import { cloudSessionsQueryKey, workspaceQueryKey } from "./useWorkspaceQuery";
import { useTerminalResetStore } from "../stores/terminal-reset-store";

export type RestoreSessionResult =
	{ status: "success" } | { status: "not_resumable"; message: string } | { status: "error"; message: string };

/**
 * A deleted cloud session stays in the cloud sessions listing (flagged
 * terminated) so it can be restored, so a session id present there marks the
 * restore as control-plane rather than local. The listing's query key carries
 * the org the sessions belong to (`[...cloudSessionsQueryKey, baseUrl, orgId]`),
 * so read the org from the matching entry rather than re-subscribing to it.
 */
function findCloudSessionOrg(queryClient: QueryClient, sessionId: string): string | undefined {
	for (const [key, sessions] of queryClient.getQueriesData<CloudCpSession[]>({ queryKey: cloudSessionsQueryKey })) {
		if (sessions?.some((session) => session.id === sessionId)) {
			const orgId = key[2];
			return typeof orgId === "string" && orgId !== "" ? orgId : undefined;
		}
	}
	return undefined;
}

export function useRestoreSession(): (sessionId: string) => Promise<RestoreSessionResult> {
	const queryClient = useQueryClient();

	return useCallback(
		async (sessionId: string) => {
			// Cloud sessions re-provision through the control plane, not the local
			// daemon: restore keeps the conversation and work intact server-side.
			const cloudOrgId = findCloudSessionOrg(queryClient, sessionId);
			if (cloudOrgId !== undefined) {
				const settings = queryClient.getQueryData<Settings>(settingsQueryKey);
				const baseUrl = settings?.cloudControlPlaneUrl ?? "";
				if (baseUrl === "") {
					return { status: "error", message: "The cloud control plane is not configured." };
				}
				try {
					await createRendererCloudCpClient(baseUrl).restoreSession(cloudOrgId, sessionId);
					// The session flips to reviving on the next fetch; refresh the cloud
					// listing (and the merged board) so the UI reflects it immediately.
					await queryClient.invalidateQueries({ queryKey: cloudSessionsQueryKey });
					await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
					// Restore re-provisions a fresh sandbox under the same session id but
					// a NEW worker epoch, so the old terminal is dead. Bump the reset nonce
					// so the pane rebuilds from scratch (new mux factory, cursor at 0) and
					// re-mints against the new epoch instead of clinging to the exited one.
					useTerminalResetStore.getState().bump(sessionId);
					return { status: "success" };
				} catch (err) {
					return {
						status: "error",
						message: err instanceof Error ? err.message : "Unable to restore session",
					};
				}
			}

			try {
				const { data, error } = await apiClient.POST("/api/v1/sessions/{sessionId}/restore", {
					params: { path: { sessionId } },
				});
				if (error) {
					const code = (error as { code?: string }).code;
					const message = apiErrorMessage(error, "Unable to restore session");
					if (code === "SESSION_NOT_RESUMABLE") {
						return { status: "not_resumable", message };
					}
					return { status: "error", message };
				}
				await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
				if (data?.restoreMode === "saved_prompt") {
					void aoBridge.notifications
						.show({
							id: `restore-fallback:${sessionId}:${Date.now()}`,
							title: "Started from saved prompt",
							body: "AO could not resume the native agent session, so it started a new conversation from the saved prompt.",
						})
						.catch((err) => {
							console.warn("Unable to show restore fallback notification", err);
						});
				}
				return { status: "success" };
			} catch (err) {
				return {
					status: "error",
					message: err instanceof Error ? err.message : "Unable to restore session",
				};
			}
		},
		[queryClient],
	);
}
