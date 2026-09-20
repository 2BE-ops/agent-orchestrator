# Adaptive agent platform: final report

Stage-27 close-out report per mission §71. Every claim below is either
referenced to a stage's recorded evidence (see
[validation](adaptive-agent-platform-validation.md) and the
[checklist](adaptive-agent-platform-checklist.md)) or marked honestly as not
live-tested. Nothing is claimed as tested that was merely implemented; no
harness combination is claimed that was not actually available.

## Fork URL

https://github.com/2BE-ops/agent-orchestrator (fork of
`Untrivial-ai/agent-orchestrator`). By decision at close-out this is a
fork-native product: **no upstream PR will be opened**; the branch keeps
one-way upstream sync (upstream → fork merges continue, nothing flows back).
Fork draft PR #1 (`Adaptive Agent Platform (stage 24 CI verification)`) is the
CI vehicle only and stays draft.

## Feature branch

`feature/adaptive-agent-platform`, tip at close-out: the stage-27 merge
`efb455fa1` (plus the final documentation commit). Base: fork `main` at
`684d6d67d`, rebased onto upstream `6d3ad8c7c` at stage 00/01.

## Starting upstream SHA

`684d6d67db004e180f7ac9c649ce92840491ea4d` (branch point), advanced to the
upstream baseline `6d3ad8c7c` before implementation began.

## Final upstream SHA incorporated

`dd531d2fb` — stage 26 merged `6d3ad8c7c..b9601f38c` (28 commits,
`de5fcedac`) and stage 27 merged `b9601f38c..dd531d2fb` (12 commits,
`efb455fa1`, with upstream's colliding `0148_notification_dismissal`
renumbered into the fork sequence as `0184`).

## Commit history

~90 conventional commits across 28 milestone stages, each with its own
build/vet/test/lint cycle and pushed to the fork (per-stage SHAs are recorded
in the checklist rows and validation sections; `git log 684d6d67d..HEAD` is
the authoritative list). Product fixes at close-out: `a5f60ca15` (opencode
permission advertisement), `b02b90723` (knowledge sources editor), plus the
`efb455fa1` upstream merge.

## Architecture summary

The platform adds a governed adaptive layer over AO's existing session/lifecycle
core, organized around sealed facts and derived reads: every durable write is
an immutable, hash-pinned fact (registry versions, task revisions, criteria,
delegation contexts, message/review/decision/receipt records, goal versions,
plan receipts, notices, experiments, controls), and every status/score/summary
is recomputed from those facts at read time — nothing derived is stored.
Enforcement is chokepoint-based: one admission transaction covers all six
worker-launch callers (global cap, per-Agent-Type `maxParallelWorkers`,
project-control fencing), so limits are SQL-impossible to bypass and
restart-safe by construction. Native agents (workers, the Agent Manager, the
orchestrator) speak versioned, golden-pinned byte protocols delivered through
one guarded native-context boundary; the daemon never trusts agent prose —
proposals are validated against exact identities, hashes and policy before any
decision, and rejections are retained with bounded correction. See the
stage-26 thematic review (`adaptive-agent-platform-review.md`) for the full
seven-theme analysis.

## Original AO functionality reused

The entire supervisor core: Cobra CLI/daemon/loopback HTTP surface, session
lifecycle and the terminal mux, worktree management, the SCM observer and PR
flows, review infrastructure, the chat-driver/ACP layer, notifications,
 Electron desktop shell and generated typed client. The adaptive layer
deliberately reuses these rather than forking them (delegation rides the
normal spawn path; the Manager controller is a registry Agent Type; knowledge
travels the normal project API).

## Original AO functionality extended

Session creation gained an admission transaction (limits + fencing) shared by
every launch caller; the task composer and board gained registry-worker
selection (suppressing legacy per-session fields when a type is pinned); the
projects API gained goal/control/config-extension routes; notifications,
audit and telemetry surfaces gained allowlisted adaptive event types; the
desktop gained seven new surfaces; harness config specs gained the
permissions-field contract (opencode fixed at close-out to advertise its
implemented modes).

## New domain entities

Registry entries (Agent Types/Skills) with immutable versions and ownership
policy; provider bindings; worker configurations/execution history; the
adaptive task DAG (tasks, revisions, frozen criteria, leases, dispatches,
intents); classified contexts and delegation artifacts; typed worker results
and task messages with durable delivery; evaluation evidence and bounded
performance cohorts; the Agent Manager (governance, controllers, inbox,
classified inputs, deliveries, proposals, decisions, decision cursor, registry
actions); context classification labels; orchestrator goal versions, plan
receipts, notices and completions; evolution experiments/recommendations;
project controls and needs-human records. Full vocabulary in
`backend/internal/domain/`.

## Database migrations

37 fork-owned migrations `0148_registry_cdc` through
`0184_notification_dismissal` (the last being upstream's colliding migration
renumbered into the fork sequence; goose's unique version ids are why). All
append-only after ship; the upgrade matrix (clean + existing v140 database
with data) is tested to head.

## New/changed APIs

~120 new routes across registry (agent-types/skills/provider-bindings CRUD,
versions, activation, audit, import/export, validation), tasks (DAG,
revisions, criteria, leases, attempts), knowledge, contexts, results,
messages, evaluations/performance, the Agent Manager family (governance,
controllers, inbox, requests, proposals, decisions, audit, attribution,
candidates), orchestrator (goal/versions/completions, native
goal/plan/complete/feedback/receipts, attribution), evolution, project
control (state, cancel, needs-human, dry-run), and settings
(max-concurrent-workers). Typed 429 envelopes for scheduler/fencing refusals.
All spec-generated (`openapi.yaml` + `schema.ts` regenerated together).

## New UI surfaces

Registry editors (Agent Types with versioning/pinning/binding, Skills with
resources/import/export), the task graph (layered DAG + list + detail), the
Manager dashboard (controller/governance/population/decisions), performance
metrics, knowledge (with the close-out sources editor), audit timeline, and
the control center (state machine, needs-human queue, bulk cancel,
experiments/recommendations); plus worker selection in the task composer.

## Agent Manager capabilities

Persistent governance (USER-only configuration, schema-1 definitions pinned
to an exact controller Type version, bounded creation/version policy); a
durable inbox with classified, generation-pinned native delivery; native
Type/Skill/candidate inspection; sealed proposals with hash-pinned
provenance; daemon-validated immutable decisions with semantic rejection and
bounded correction; governed dynamic Type/Skill authoring (quotas, idempotent
receipts, manager-origin registry listing); attribution summaries. Live at
close-out: admission, candidate scan, proposal, rejection+correction,
accepted selection and resolution — all driven by a real GLM manager.

## Agent Type capabilities

Manual authoring with immutable versions, skills pinning, provider bindings,
per-type `maxParallelWorkers`, session-mode/permission/model configuration,
validation with readiness gating, activation/rollback/clone/audit, and
manager-select/modify/version policy. Registry launches pass the same
admission gates as every other launch caller.

## Skill capabilities

Bounded resources, tool/MCP requirements, exact-version portable
bundles, atomic import with unresolved-binding refusal, enable/disable with
retained reasons, version history, and Manager-governed authoring.

## Task DAG architecture

Immutable task revisions with parent/dependency edges validated in-transaction
(cycle/depth/scope); frozen acceptance criteria as the routing precondition;
exclusive leases and one-dispatch-per-attempt (SQL-protected); intents for
audited run/cancel; needs-human ancestry fencing; read-time derived states;
the dry-run simulator re-using the write path's validation over an overlay.

## Evaluation architecture

Immutable attempt evidence (CI/test/build/lint checks, committed artifacts,
mergeability, native reviews with generation fencing); evaluator recompute
never trusting caller evidence; current-completion invalidation on new facts;
bounded 366-day/1000-member performance cohorts with confound (mixed-version,
shared-task) exclusion; eight grouping dimensions; routing/planning outcome
attribution derived per request/receipt.

## Context/knowledge architecture

Context classification (technical/engagement/mission) enforced at every seam
(delegation payloads, manifests, knowledge selection, Manager input); sealed
per-attempt contexts with provenance; the bounded builder feeding native
launch/restore; versioned knowledge with 1–16 structured provenance sources,
status/confidence lifecycle and pinned selection.

## Tests added

Roughly 700 new backend tests (domain, store, services, controllers, CLI,
apispec, telemetry) and ~250 frontend tests across the new components and
client; golden-pinned native protocol byte tests; SQL-level admission/race
proofs; a zero-mutation dry-run proof; the clean/existing-DB upgrade matrix.

## Exact test results

Final tree: `go build`/`go vet` clean (vet: pre-existing POSIX `syscall.Kill`
test file only); full `go test ./...` = 37 documented Windows-environmental
failing packages proven identical at the pre-merge control tip plus one new
environmental entry (agentauth kimi trust seeding) — all green on Linux CI;
pinned golangci-lint v2.12.2 = exactly the six documented Windows-only
findings; sqlite full suite green including the renumbered-0184 ledger and
upgrade matrix; frontend `tsc --noEmit` clean; vitest 5046 pass / 150 fail /
8 skip (failures exactly the recorded mac-signing/DMG/blockmap/main-process
Windows classes; CI runs them green); sqlc + OpenAPI/TS regeneration drift
zero. Race suites: CI-only by design (no local C compiler — recorded, never
claimed). Full per-stage numbers are in the validation doc.

## Build results

Backend `go build ./...` green locally and on CI; desktop packaging produced
the real NSIS installer via the exact CI command (stage 24, no publish); the
stage-27 lab built the daemon/renderer and ran the dev stack end to end.

## Live UI validation performed

Stage 25 (real Electron window, isolated worktree/scratch state): project
import, Skill authoring, Agent-Type authoring with pinned skills and version
activation, registry + board launches, the fake timeline through its full
lifecycle, per-type limit refusals (in-dialog + 429), pause→resume, daemon
hard-kill restart reconciliation, an authenticated opencode+GLM chat turn, and
all seven surfaces. Stage 27 (same isolation): the opencode registry type
passed pre-flight and launched with a real GLM reply; sourced knowledge
creation; the goal loop (sealed goal → plan receipt → DAG task); manager
admission and full Automatic selection including rejection+correction; the
orchestrator launching the selected worker whose edit met the frozen
criterion; concurrent heterogeneous workers; daemon replacement; all surfaces
via live read models (the window itself ran healthy but DWM capture returned
a black raster — recorded, with API/store evidence standing in for pixels).

## Harness/provider combinations actually tested

Fake harness (deterministic timeline; live 25/27). opencode 1.18.31 via the
local zai-sub GLM bridge (custom provider; GLM-5.3-Flash): chat turn (25),
registry launch, orchestrator, manager and autonomous worker turns (27).
claude-code as delegate orchestrator (idle session, 25). Native Codex
reply-only chat turn (09c4). No other harness or provider is claimed.

## Features implemented but not live-tested

Needs-human raise/resolve and dry-run flows (tested at service/UI level,
viewed live empty); native Manager `create_experiment`/`create_recommendation`
tools (the service path is exposed; the native tools are deferred); live
heterogeneous review (evaluator paths tested); live experiment execution
(lists live-empty); the dedicated adaptive-off live toggle.

## Known limitations

Live validation is Windows-only; race coverage exists only on CI; the
Electron-window screen capture on this machine yields black frames (the app
runs; evidence rides the API and session stores); the zai-sub bridge's
provider-config auth is invisible to `opencode auth list`, so lab readiness
needed a temporary credential (removed); opencode sessions without an explicit
permission mode pause on first native command (default-ask), worked around in
the lab via the project orchestrator override.

## Human-required actions

None for the delivered state. Optional follow-ups: acknowledge/adjust the
React Doctor renderer annotations; decide whether the deferred native Manager
evolution tools should ship; periodically merge `upstream/main` (one-way sync
continues to work — proven twice).

## Upstream divergence

40 upstream commits incorporated through `dd531d2fb` with no speculative
overwrite; the only recurring semantic seam is the task composer (resolved
twice by combining the registry worker-selection guards with upstream's
composer changes). Upstream's `0148_notification_dismissal` is renumbered
`0184` in the fork (goose version-id collision; upstream databases were never
a supported fork upgrade path past the stage-26 merge base). Divergence is
deliberate and one-way from here.

## Technical debt

Store breadth (231 storage files — the sealed-facts model's honest cost);
unindexed adaptive aggregate reads at current bounds; protocol renderers
accumulated v1–v4 (consolidation owed before a v6); session_manager's eight
and controllers' two Windows test baselines (environmental, Linux-green);
agent-ci's Windows tar path bug; the manager's id-less-envelope rejection in
the 27 lab traces to driver-instruction looseness, but a protocol-level
candidate-shape example in the using-ao docs would prevent agents from
repeating it.

## Recommended next improvements

Consolidate the native protocol renderers behind one versioned encoder;
index the outcome-attribution aggregates before cohort growth demands it;
ship the native Manager evolution tools over the tested service path; add
needs-human/dry-run to the live-lab repertoire; revisit worker-facing
read-receipts for task messages (currently a documented transport-level
boundary); keep the upstream merge cadence (twice-proven playbook).
