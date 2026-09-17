import { create } from "zustand";
import type { TelemetryPolicyView } from "../../shared/telemetry-policy";
import { aoBridge } from "../lib/bridge";

type TelemetryPolicyState = {
	view: TelemetryPolicyView | null;
	loaded: boolean;
	saving: boolean;
	saveError: boolean;
	githubIdentitySaving: boolean;
	githubIdentitySaveError: boolean;
	load(): Promise<void>;
	setEnabled(enabled: boolean): Promise<void>;
	setGithubIdentityEnabled(enabled: boolean): Promise<void>;
};

let pendingLoad: Promise<void> | null = null;
let subscribed = false;

export const useTelemetryPolicyStore = create<TelemetryPolicyState>((set, get) => ({
	view: null, loaded: false, saving: false, saveError: false, githubIdentitySaving: false, githubIdentitySaveError: false,
	load: async () => {
		if (get().loaded) return;
		if (pendingLoad) return pendingLoad;
		pendingLoad = (async () => {
			if (!subscribed) {
				subscribed = true;
				aoBridge.telemetry.onPolicy((view) => {
					if (!view.eventsEnabled && typeof localStorage !== "undefined") {
						localStorage.removeItem("ao.telemetry.activeSlotsByDate");
						localStorage.removeItem("ao.telemetry.routeViewsByDate");
					}
					set({ view, loaded: true, saving: false });
				});
			}
			try { set({ view: await aoBridge.telemetry.getPolicy(), loaded: true }); }
			catch { set({ loaded: true, saveError: true }); }
		})();
		try { await pendingLoad; } finally { pendingLoad = null; }
	},
	setEnabled: async (enabled) => {
		if (get().saving) return;
		set({ saving: true, saveError: false });
		try { set({ view: await aoBridge.telemetry.setEventsEnabled(enabled), loaded: true, saving: false }); }
		catch { set({ saving: false, saveError: true }); }
	},
	setGithubIdentityEnabled: async (enabled) => {
		if (get().githubIdentitySaving) return;
		set({ githubIdentitySaving: true, githubIdentitySaveError: false });
		try { set({ view: await aoBridge.telemetry.setGithubIdentityEnabled(enabled), loaded: true, githubIdentitySaving: false }); }
		catch { set({ githubIdentitySaving: false, githubIdentitySaveError: true }); }
	},
}));
