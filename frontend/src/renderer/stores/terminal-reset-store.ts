import { create } from "zustand";

// A per-session counter bumped whenever a session's terminal must be rebuilt
// from scratch rather than reconnected to the running one. Restore is the case
// that needs it: the control plane re-provisions a fresh sandbox under the SAME
// session id but a NEW worker epoch, so the old terminal is dead and its replay
// cursor is meaningless. The cloud terminal is otherwise keyed only on the
// (unchanged) session id, so without a reset signal the pane keeps its cached
// mux factory (and its stale cursor) and never re-mints against the new epoch —
// the "connected but TERMINAL ENDED / can't type" symptom after restore.
//
// Folding this nonce into the terminal cache key and the cloud mux factory key
// makes a bump behave exactly like a fresh open: a new pane mounts and a new
// factory closure is created with a fresh cursor at 0, so the rebuilt mux dials
// `after=0` and replays the new epoch cleanly. Not persisted — it only matters
// within a live session where a terminal is currently mounted.
type TerminalResetState = {
	nonces: Record<string, number>;
	// Sessions that are reconnecting to a fresh box (restore/resume) and have not
	// re-attached yet. This is the IMMEDIATE, synchronous signal a restore sets:
	// the polled runtimeConnected lags up to the 5s cloud poll, so on its own it
	// leaves a window where the previous box's dead terminal still shows after a
	// restore click. `bump` marks the session reconnecting; the terminal clears
	// it via `markAttached` the instant it attaches to the new box.
	reconnecting: Record<string, boolean>;
	bump: (sessionId: string) => void;
	markAttached: (sessionId: string) => void;
};

export const useTerminalResetStore = create<TerminalResetState>((set) => ({
	nonces: {},
	reconnecting: {},
	bump: (sessionId) =>
		set((state) => ({
			nonces: { ...state.nonces, [sessionId]: (state.nonces[sessionId] ?? 0) + 1 },
			reconnecting: { ...state.reconnecting, [sessionId]: true },
		})),
	markAttached: (sessionId) =>
		set((state) =>
			state.reconnecting[sessionId]
				? { reconnecting: { ...state.reconnecting, [sessionId]: false } }
				: state,
		),
}));

/**
 * Reads a session's current terminal-reset nonce without a React subscription,
 * for hook-free callers (e.g. deriving a cache key inside a memo). Returns 0
 * when the session has never been reset.
 */
export function terminalResetNonce(sessionId?: string): number {
	if (!sessionId) return 0;
	return useTerminalResetStore.getState().nonces[sessionId] ?? 0;
}
