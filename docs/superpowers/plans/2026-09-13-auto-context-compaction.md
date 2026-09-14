# Automatic Context Compaction Implementation Plan

**Goal:** Automatically compact supported Chat conversations before they exhaust their model context, while exposing one safe, capability-driven manual Compact action for both Chat and TUI sessions. The behavior must apply identically to orchestrators and workers.

**Architecture:** Context accounting and compaction policy belong to the daemon. Providers report current context occupancy; the Chat controller persists the latest measurement and runs auto-compaction at a serialized turn boundary. The renderer displays daemon-owned state and invokes one semantic session action. TUI compaction remains manual and is implemented only by adapters that can prove an idle, empty composer and supply a verified native command.

**Initial product policy:**

- Chat auto-compaction is enabled by default for providers advertising both `usage` and `compaction`.
- Trigger at 80% context occupancy and re-arm only after occupancy falls below 60%.
- Keep `/compact` and “Compact now” as manual recovery paths.
- Never automatically compact TUI sessions in the first release.
- Show TUI compaction only for individually verified harness adapters; unsupported harnesses expose no action.
- A compaction must never interrupt an active turn, answer a decision, overwrite a terminal draft, or race an interface/agent/account transition.

---

## Task 1: Define the cross-mode compaction contract

**Files:**

- Modify: `backend/internal/ports/chat.go`
- Modify: `backend/internal/ports/agent.go`
- Modify: `backend/internal/domain/conversation.go`
- Create tests beside the affected domain and port packages

- [ ] Keep `ports.ChatCompactor` as the structured Chat execution capability.
- [ ] Add an optional TUI adapter capability such as:

```go
type TUICompactionSpec struct {
	Command string
}

type AgentTUICompactionProvider interface {
	TUICompaction(context.Context, LaunchConfig) (TUICompactionSpec, bool, error)
}
```

- [ ] Keep the native command daemon-side. Never return `/compact`, `/compress`, or another provider command to the renderer.
- [ ] Add domain vocabulary for `manual` versus `automatic` triggers and `requested`, `accepted`, `completed`, `failed`, and `interrupted` attempt states.
- [ ] Define typed unsupported, busy, unsafe-composer, unknown-context, and transition-in-progress errors.
- [ ] Document that session kind is irrelevant: an orchestrator and worker use the same session compaction contract.

## Task 2: Persist automatic policy and attempt state

**Files:**

- Create: `backend/internal/storage/sqlite/migrations/0130_auto_context_compaction.sql`
- Modify: `backend/internal/storage/sqlite/queries/settings.sql`
- Modify: `backend/internal/storage/sqlite/queries/conversations.sql`
- Modify: `backend/internal/storage/sqlite/store/*_store.go`
- Regenerate: `backend/internal/storage/sqlite/gen/*` with `npm run sqlc`

- [ ] Add global settings with defaults:
  - `chat_auto_compaction_enabled = 1`
  - `chat_auto_compaction_threshold_percent = 80`
  - `chat_auto_compaction_rearm_percent = 60`
- [ ] Add a `conversation_compaction_attempts` table containing the conversation, controller generation, manual/automatic trigger, state, trigger usage/window, threshold, optional provider turn, timestamps, and a bounded failure reason.
- [ ] Add a partial unique index allowing at most one `requested` or `accepted` attempt per conversation.
- [ ] Write the `requested` record before calling the provider so a daemon crash cannot forget an accepted side effect.
- [ ] Treat a stale `requested` or `accepted` attempt as interrupted during controller reconciliation unless replayed provider history proves completion.
- [ ] Preserve the existing latest-wins `context_used` and `context_window` columns; do not derive fullness from cumulative input/output totals.
- [ ] Add store tests for settings defaults, state transitions, restart recovery, and generation fencing.

## Task 3: Expose daemon-owned settings and session capability

**Files:**

- Modify: `backend/internal/service/settings/service.go`
- Modify: `backend/internal/httpd/controllers/settings.go`
- Modify: `backend/internal/httpd/controllers/dto.go`
- Modify: `backend/internal/httpd/apispec/specgen/build.go`
- Modify: session/workspace DTO projection files
- Regenerate: `backend/internal/httpd/apispec/openapi.yaml`
- Regenerate: `frontend/src/api/schema.ts`

- [ ] Add `PATCH /api/v1/settings/auto-compaction` for enabled/threshold settings.
- [ ] Add one semantic manual endpoint: `POST /api/v1/sessions/{sessionId}/compact`.
- [ ] Keep the existing Chat conversation endpoint temporarily for compatibility and route both paths through the same service operation.
- [ ] Project an action descriptor with the session snapshot, for example:

```json
{
  "compaction": {
    "supported": true,
    "automatic": true,
    "contextUsed": 206720,
    "contextWindow": 258400,
    "thresholdPercent": 80,
    "state": "ready"
  }
}
```

- [ ] For TUI, omit context figures unless an adapter supplies authoritative values. Unknown must not render as 0%.
- [ ] Cover response shapes, validation, error envelopes, request IDs, and route/spec parity.

## Task 4: Implement the Chat auto-compaction state machine

**Files:**

- Modify: `backend/internal/service/chat/controller.go`
- Modify: `backend/internal/service/chat/service.go`
- Modify: `backend/internal/service/chat/controller_test.go`
- Extend: `backend/e2e/chat_compaction_test.go`

- [ ] Continue consuming provider usage events into `ConversationUsage.ContextUsed` and `ContextWindow`.
- [ ] Compute fullness only when `ContextWindow > 0` using the existing `ContextFraction()` helper.
- [ ] Add a controller-local `compactionPending` fence before calling the provider. Include it in the controller busy/queue decision so a user send cannot enter the provider during the request-to-turn-start gap.
- [ ] At a primary turn completion, run `maybeAutoCompactLocked` before `drainLocked`:

```text
settle turn
  -> clear primary turn ownership
  -> evaluate policy and current usage
  -> compact, or dispatch the next queued turn
```

- [ ] If a usage event crosses the threshold while the controller is already idle, schedule the same serialized check rather than waiting for another user turn.
- [ ] Require all of the following: setting enabled, `usage` and `compaction` capabilities, known context window, threshold crossed, controller idle, no pending interaction, no handoff, no active attempt, and re-arm/cooldown satisfied.
- [ ] On provider acceptance, retain the compaction fence and leave queued turns durable.
- [ ] On the provider compaction event and its turn completion, mark the attempt completed, refresh the context measurement, release the fence, and drain exactly one queued turn.
- [ ] On refusal/failure, mark the attempt failed, release the fence, drain queued work, and suppress retries at the same usage watermark until the cooldown or a materially newer measurement.
- [ ] Treat provider-initiated compaction as a valid completion and reset the automatic trigger.
- [ ] Test threshold boundaries, unknown windows, duplicate usage events, sends racing acceptance, queued turns, manual/automatic races, handoffs, provider failures, provider-initiated compaction, and daemon restart recovery.

## Task 5: Add safe manual TUI compaction

**Files:**

- Modify: `backend/internal/session_manager/manager.go`
- Modify: `backend/internal/sessionguard/guard.go`
- Modify: selected adapters under `backend/internal/adapters/agent/`
- Add focused tests in the same packages

- [ ] Resolve the session and require committed TUI mode, a live exact runtime generation, and a harness implementing `AgentTUICompactionProvider`.
- [ ] Acquire the existing exclusive session-operation fence and terminal input drain before inspecting or writing.
- [ ] Require an idle activity state and reject blocked/active/startup-pending states.
- [ ] Capture current terminal output and require `EmptyComposerDetector` to positively prove that no human draft is present.
- [ ] Add a dedicated guarded maintenance write that rechecks session state and exact runtime generation immediately before `SendMessage`.
- [ ] Submit the adapter-declared native command plus Enter. Do not route through ordinary `Manager.Send`, because that path records a user prompt and applies orchestrator message rewriting.
- [ ] Report `accepted` when terminal delivery succeeds. Report `completed` only if that adapter later supplies authoritative completion evidence; terminal text alone is not a universal acknowledgement.
- [ ] Start with Codex and Claude Code only after live command/version verification. Add other harnesses one at a time with recorded tests. Do not create a default command for all harnesses.
- [ ] Keep automatic TUI compaction out of scope until an adapter can provide both authoritative context occupancy and safe completion evidence.

## Task 6: Build the shared desktop experience

**Files:**

- Modify: `frontend/src/renderer/components/SessionView.tsx`
- Modify: `frontend/src/renderer/components/CenterPane.tsx`
- Modify: `frontend/src/renderer/components/chat/SessionChatSurface.tsx`
- Modify: `frontend/src/renderer/components/chat/ChatComposer.tsx`
- Modify: `frontend/src/renderer/components/chat/ChatCompaction.test.tsx`
- Add: focused `SessionView`/`CenterPane` tests
- Modify: `frontend/src/renderer/components/settings/GeneralSettingsSection.tsx`
- Modify: `frontend/src/renderer/hooks/useSettings.ts`
- Modify: `frontend/src/renderer/i18n/en.json`

- [ ] Add a shared context control to session chrome, rendered for the primary agent tab only—not shell, reviewer, browser, or file tabs.
- [ ] In Chat mode, show the exact context percentage when known, automatic threshold status, a settings affordance, and “Compact now.”
- [ ] In TUI mode, show only the manual action when the daemon advertises verified support. Explain unsafe/busy refusals without injecting terminal input.
- [ ] Use the same component for orchestrators and workers; do not branch on `session.kind`.
- [ ] Keep `/compact` as a Chat composer shortcut, but gate it with `can(snapshot, "compaction")` before rendering. Remove the current probe-by-failure behavior.
- [ ] Replace the test that requires “no toolbar button” with capability, placement, busy, automatic, and TUI safety tests.
- [ ] Add global settings for automatic Chat compaction and threshold. Keep 80% as the default and constrain custom values to a safe range such as 60–90%.
- [ ] Announce state changes through `aria-live`, preserve keyboard access, and provide a visible label or tooltip for icon-only compact layouts.

## Task 7: Validate contracts and behavior

- [ ] Run focused Go tests for domain, store, Chat controller, session guard, session manager, and HTTP controllers.
- [ ] Run focused frontend tests for Chat compaction, SessionView, CenterPane, settings, and capability mapping.
- [ ] Run `npm run sqlc` after query/schema changes.
- [ ] Run `npm run api` and commit both generated API artifacts.
- [ ] Run `cd backend && go test ./internal/httpd/... ./internal/service/chat/... ./internal/session_manager/...`.
- [ ] Run `npm run frontend:typecheck` and `cd frontend && npm run build`.
- [ ] Run `npm run lint` and then the broader CI-equivalent suites required by `AGENTS.md`.
- [ ] Perform live acceptance with a Codex Chat orchestrator, Codex Chat worker, queued message crossing 80%, daemon restart during compaction, and verified Codex/Claude TUI sessions.
- [ ] Use `ao preview` from inside the session for final desktop visual verification.

## Recommended rollout

1. Ship automatic Codex Chat compaction and the shared context indicator behind a temporary feature flag.
2. Observe duplicate-attempt, failure, and context-exhaustion diagnostics without recording transcript content.
3. Enable by default after restart and queue-race acceptance passes.
4. Add verified TUI adapters incrementally.
5. Add ACP structured compaction only when the protocol/provider exposes a real operation; do not emulate it with an ordinary prompt.
