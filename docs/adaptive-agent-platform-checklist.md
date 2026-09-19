# Adaptive agent platform implementation checklist

Last updated: 2026-09-19. Starting upstream commit:
`684d6d67db004e180f7ac9c649ce92840491ea4d`.

This is the persistent execution plan for the assignment. Registry persistence
is implemented and tested; the complete feature is not yet implemented. The [design](adaptive-agent-platform-design.md) records source
findings and proposed boundaries; [validation](adaptive-agent-platform-validation.md)
records exact environment/test evidence.

Statuses: **NOT STARTED**, **IN PROGRESS**, **IMPLEMENTED**, **TESTED**,
**VALIDATED IN RUNNING APP**, **BLOCKED**. IMPLEMENTED is not a completion claim;
TESTED requires recorded command results; running-app validation requires an
observed real-app flow. Source audit does not count as adaptive feature testing.

## Environment facts (verified 2026-09-18, machine `bill`)

- Repo: `C:\Users\bill\Desktop\agent-orchestrator`, branch
  `feature/adaptive-agent-platform`, rebased onto upstream `6d3ad8c7c`
  (migrations verified to still end at `0147`; the new upstream commit was
  frontend-only).
- `origin` = `https://github.com/2BE-ops/agent-orchestrator` (fork, pushes work
  as `2BE-ops` via `gh`); `upstream` = `Untrivial-ai/agent-orchestrator`.
- Go: `C:\Users\bill\go-sdk\go\bin` on persistent user PATH; GOROOT set. First
  build downloads go1.26.5 automatically per `go.mod` toolchain line.
- Pinned tools need no installation: `npm run lint` (golangci-lint v2.12.2) and
  `npm run sqlc` (sqlc v1.31.1) self-provision via `go run` from the repo root.
- Frontend API contracts: `npm run api` (specgen + openapi-typescript) at repo
  root; typed client is `frontend/src/renderer/lib/api-client.ts`.
- The full original assignment is preserved verbatim in
  [adaptive-agent-platform-mission.md](adaptive-agent-platform-mission.md).

## Current execution gate

None. The earlier `.git` read-only restriction belonged to the previous
(sandboxed) session, not to this machine. On 2026-09-18 the work resumed from an
unsandboxed shell: the dead GitHub credential was removed, git pushes route
through `gh` as account `2BE-ops`, the fork
`https://github.com/2BE-ops/agent-orchestrator` was created, remotes were wired
(`origin` = fork, `upstream` = `Untrivial-ai/agent-orchestrator`), and
`feature/adaptive-agent-platform` was created from `684d6d67db004e180f7ac9c649ce92840491ea4d`.
Stage 03 completes with this commit.

## Milestones

| Stage | Scope | Status | Evidence / next action |
| --- | --- | --- | --- |
| 00 | Inspect worktree/remotes/base | TESTED | Clean initial checkout, `main`, original `origin`, exact SHA recorded |
| 01 | Fork, upstream/origin, feature branch | TESTED | Fork `2BE-ops/agent-orchestrator` created via `gh`; `origin`/`upstream` verified; branch cut from starting SHA |
| 02 | Current-state source and upstream audit | IMPLEMENTED | Lifecycle, config, persistence, native skills, reviews, UI and overlap documented |
| 03 | Gap analysis/design and live checklist commit | TESTED | Design/checklist/validation committed on feature branch and pushed to fork |
| 04 | Reproducible baseline/toolchain | TESTED | Go 1.25.7 installed (`C:\Users\bill\go-sdk\go`, on user PATH, GOROOT set); `go build ./...` clean (toolchain auto-selects 1.26.5 per go.mod); workspace `node_modules` restored (`product-ui`, `cloud-client`, `mobile`, `ao`); `frontend:typecheck` PASS; `product-ui:check` (typecheck+test+build) PASS; pinned sqlc/golangci-lint self-provision via root scripts |
| 05 | Registry domain, immutable versions, migration/store | TESTED | Migrations 0148–0149, typed definitions and sqlc store. Full domain/SQLite/CDC suites, relevant pinned lint (0 issues), vet and backend build PASS. Eleven new top-level tests cover validation, ownership, concurrent revisions, pins, atomic audit/CDC rollback, upgrade and restart. Race run unavailable: GCC missing. Exact evidence in validation. |
| 06 | Agent Type service/API/CLI and registry UI | VALIDATED IN RUNNING APP | Shared service, 20 routes, generated contracts, HTTP-only CLI, desktop editor and CDC refresh. Registry API/CLI, relevant lint, frontend typecheck and 174 focused tests PASS; 40 topbar tests PASS. Real Electron create/pin/version/compare/activate/rollback/audit/clone/disable/reload verified. Full-suite Windows/dependency failures recorded in validation, not labeled passed. Stage 05 commit `1be075cf6` pushed. |
| 07 | Skill authoring/versioning/API/UI/import/export | VALIDATED IN RUNNING APP | Bounded resources/tool/MCP requirements, exact-version portable bundles, atomic disabled imports and CLI/API/UI implemented. Round-trip/strict-input/rollback tests, 44 frontend tests, typecheck, touched lint, backend and renderer builds PASS. Real Electron resource/version/export download/file import/policy flow and full daemon restart persistence verified. Native materialization/temporary attachments remain stage 09 integration. Stage 06 pushed as `c4c03aa5b`. |
| 08 | Provider/capability integration and binding UI | VALIDATED IN RUNNING APP | Native ConfigSpec/model/readiness integration, mode-aware Chat inspection, immutable-target provider references, migration 0150, strict API and desktop binding editor/checks. Domain/registry/SQLite/Codex/spec suites, focused API tests, 46 frontend tests, typecheck, builds, vet and changed-code lint PASS. Real Electron create/bind v3/disable rejection/re-enable/readiness/reload verified. Custom-provider fixtures pass; live custom-provider and worker launch evidence remains stages 09/25. Baseline Windows suite/lint gaps recorded. Stage 07 pushed as `ac7e7cc26`. |
| 09 | Worker snapshots and manual Agent Type launch | TESTED | Pushed through 09c4 `dd348ec1e`; 09c5 completes recoverable provider-owned controls (0153), retained native settings, dispatch/ownership fences and queue recovery. Shared launch/restore/resources, CLI/API/composer, interface/harness/per-turn execution history and live inspector complete. Full domain/registry/SQLite/store/Chat/session, focused manager, API-spec, build/vet/typecheck/changed-code lint and 40 latest frontend tests PASS. Native Codex reply-only turn, effort/history, renderer and full desktop/daemon restart validated in 09c4. Broader live combinations and empty never-prompted native thread recovery remain stage 23/25; Windows baseline suite gaps recorded. |
| 10 | Task DAG, revisions, criteria and leases | TESTED | Pushed through 10d2 `ec7d2a9b1`; 10e adds audited run/cancel intent (0158), descendant admission fences, derived planning/lease/cancellation state, pending native operation inspection, API/CLI. Five intent-store tests, two state tests, HTTP/CLI, full domain/SQLite/task/CLI/spec suites, native task regression, 38 frontend tests, typecheck/build/lint PASS. Known Windows HTTP/manager gaps retained. Review/completion facts depend on 12/13; shared scheduling/reconciliation/control cleanup and live graph remain 16/20/21/23/25. |
| 11 | Knowledge, bounded context and manifests | TESTED | Persistence `94e6a1270` and builder/native consumption `51ef12a3e` pushed. 11c3 adds scoped context API/CLI inspection, contracts and telemetry redaction. Focused HTTP/CLI, full task/CLI/telemetry/router/spec/envelope, 39 frontend tests, typecheck/build/lint PASS. Windows gaps recorded precisely. Findings/contracts extend context in 12; desktop inspection and live provider context remain 21/22/25. |
| 12 | Worker result schema and typed communication | TESTED | Context enrichment `1f1784e84` pushed. 12e seals native output instructions, literal argv/run-file environment and generation-fenced JSON examples; updates using-ao catalog. Both native modes submit through real SQLite/shared services; idempotency, stale ownership and budget refusal PASS. Full task/context/message/Chat/store/domain/ports/CLI/skillassets, build/lint PASS. Full manager retains eight Windows baselines. Replacement-generation instruction delivery remains 23, uncertain recipient resolution 20, live provider consumption 25. |
| 13 | Evaluator and attributable performance history | TESTED | Attempt evidence pushed as `e949dc55e`. 13d2b adds complete bounded cohorts, eight grouping dimensions, retained denominators/usage samples/configuration exclusions, API/CLI and generated typed contracts. Full domain/ports/SQLite/store/task/CLI/spec/telemetry, backend build/typecheck, 39 frontend tests and affected pinned lint PASS (0 issues). Full HTTP retains only two recorded Windows baselines. Evaluator/current completion and version attribution foundations are tested; scheduler admission remains 16, recovery 23, desktop metrics 22 and live provider evidence 25. Next: persistent native Agent Manager in 14. |
| 14 | Persistent Agent Manager service/controller/tools | TESTED | Durable native claims pushed as `c93ba87d7`. 14e3 wires the post-reconciliation inbox consumer, single native admission, fair durable scans, conservative recovery and generation-specific versioned CLI protocol. Exact input/delivery API/CLI history and telemetry redaction are exposed. Real SQLite consumer, native proposal race, boundary/fairness/unknown-start tests PASS; full storage/domain/service/CLI/spec/telemetry/skills, 40 frontend tests, build/typecheck/API/lint PASS. Full HTTP/daemon retain recorded Windows baselines. Semantic selection/evolution remains 15, uncertain controls/recovery 20/23 and live demonstration 25. |
| 15.0 | Context classification foundations | TESTED | Migration 0173 plus immutable task/knowledge labels and exact pinned Type clearance. Knowledge class/scope filters precede limits; schema-v2 manifests classify all items and atomically seal numbered exact-input delegation artifacts. Violations cover live Type activation, higher tasks, forged labels/omission metadata, cross-engagement input, downward results/contracts and candidate laundering; technical output flows up. Hash-preserving upgrade/downgrade refusal, restart, rollback and native-before-launch receipts PASS. Full domain/ports/SQLite/store/services/spec suites, build/typecheck/API/sqlc and pinned affected lint PASS. Full session-manager/HTTP retain only recorded Windows baselines. Stage 15 integrates Manager/UI/public artifact history/live message and review contexts; generation replacement receipts remain 23. |
| 15 | Automatic selection/composition/dynamic creation and classification integration | IN PROGRESS | TESTED 15a-c: scoped compatibility tools; immutable transactional decisions (0176); automatic native assessment, bounded policy-race refresh, shared parser/semantic corrections, terminal feedback, fair durable recovery (0177), public history and immutable protocol v1/v2/v3. TESTED 15d storage: governed Manager registry authoring (0178) — atomic create/append_version through the shared identity/pin/revision/audit writes, idempotent sealed receipts (replay returns the original, changed payload is a conflict), 64-action request bound plus per-kind creation/version quotas, technical-conversation fencing, native provenance insert trigger, immutability/retention triggers and history-guarding Down; receipt page bound tightened to 1–100. A stray uncommitted 0179 column-based classification draft (superseded by 0173's committed JSON design, referenced by no query or generated code) was deleted before any commit. TESTED 15e wiring: Manager registry-authoring service (session-derived native attribution, daemon-minted registry identity, 1–100 receipt pages), native protocol v4 (immutable chained renderer adding registry-author/registry-receipts/registry-receipt over retained v1/v2/v3 bytes, golden-pinned), daemon dispatcher bumped to v4, HTTP session-scoped submit + project-scoped receipt history with strict page queries, thin CLI commands, telemetry allowlist entries, using-ao protocol documentation, and a proof that manager-authored definitions appear in the normal registry listing with manager origin (DoD 18 at API level). Service/domain/CLI/telemetry/skillassets suites, new controller tests, apispec/specgen, frontend typecheck, backend build and touched pinned lint (0 issues) PASS; HTTP/daemon retain only their recorded Windows baselines. TESTED 15f: public delegation history — attempt-scoped immutable sealed-context receipts exposed through the task service (task/attempt scope recheck), HTTP attempt routes, thin CLI, telemetry entries and generated contracts; task selectors already exist through task creation RequestedWorker and classified inspector reads through the attempt context/messages surfaces. Remaining stage-15 fragments: approval-bound surfaces land with stage 19 experiments/promotion and stage 20 controls; desktop selector/graph surfaces land with stages 21–22; live message/review gate evidence is stage 25. Scheduler consumption is stage 16; live demonstration is 25. |
| 16 | Shared scheduler and restart-safe dispatch | TESTED | Deterministic storage-level admission (0179): the daemon-wide concurrent worker cap lives in app_settings (default 100, 1–1000, PATCH /settings/max-concurrent-workers, generated contracts) and is enforced inside every session-creation transaction — plain CreateSession, configured Agent-Type launches and task-leased dispatches — so all six launch callers (manual HTTP spawn, session service, delegation, review launcher, Manager controller admission, tracker observer) pass one gate under the single-writer lock with no in-memory state to drift. Per-Agent-Type maxParallelWorkers is enforced from each launch's pinned WorkerConfiguration snapshot in the same transaction. Counts derive from durable session rows: worker termination frees capacity immediately, restart changes nothing, and lowering the cap never kills a worker. Orchestrator/agent_manager sessions never consume worker capacity. Refusals surface as typed 429 envelopes (SCHEDULER_WORKER_LIMIT / SCHEDULER_AGENT_TYPE_LIMIT) through the existing spawn error path. No blind reassignment: one live lease per task and one dispatch per attempt remain SQL-protected (0156), dispatch replays return the existing session before any creation code, so admission can never block a legitimate retry. Five new admission tests (cap/free/raise/lower, 8-way concurrent spawn race at cap 4, per-type limit with unrelated-type exemption, reopen-at-same-dir restart, settings bounds) plus full SQLite/store/settings/session/apispec suites PASS; controllers and daemon retain only their recorded Windows baselines; touched pinned lint 0 issues. Queueing/backpressure beyond typed refusals remains with stage 17/20 loop controls; desktop settings-field visual check is 25. |
| 17 | Orchestrator protocol and autonomous feedback loop | NOT STARTED | Durable plan/actions; bounded retries; completion against criteria |
| 18 | Manager/orchestrator outcome attribution | NOT STARTED | Routing/planning metrics distinct from worker metrics |
| 19 | Experiments, recommendations and evolution | NOT STARTED | Version diffs, comparable cohorts, explicit promotion policy |
| 20 | Dry-run, Needs Human and project controls | NOT STARTED | Deterministic behavior, independent DAG branches, race tests |
| 21 | Task graph and Agent Manager dashboard | NOT STARTED | Accessible graph/list, decisions and active worker population |
| 22 | Performance/knowledge/audit/control-center UI | NOT STARTED | Loading/error/empty/disabled states and navigation |
| 23 | Failure/recovery integration and upgrade matrix | NOT STARTED | Clean DB, existing DB, active-worker daemon/desktop restart; journal replacement-generation output instructions, including surviving Chat host adoption, without rewriting sealed context or allowing stale identity refresh. Reconcile pinned native task reviewers and uncertain launches; the generic restore path deliberately refuses to substitute live configuration for retained task context. |
| 24 | Full CI-equivalent checks/backend/desktop builds | NOT STARTED | Exact results per job; no publishing |
| 25 | Real Electron validation and harness evidence | NOT STARTED | Isolated checkout/data, fake harness first, authenticated combinations separately |
| 26 | Upstream update, whole-diff review and documentation | NOT STARTED | Fetch/assess upstream; no speculative overwrite |
| 27 | Final commits, fork push and Definition of Done report | NOT STARTED | Only after all acceptance evidence exists |

### Follow-up validation gaps (do not block independent implementation)

- Windows `go test -race` requires a C compiler. Explicit `CGO_ENABLED=1`
  reports `gcc` missing; record as NOT RUN, not passed. Resolve before stage 24
  or run the complete race suites on a supported CI/Linux environment.
- The full pinned linter finds existing Windows-specific failures outside stage
  05: `persistenthost/host_race_test.go` references `syscall.Kill`, and process
  helpers have errcheck/errorlint/gosec findings. Relevant stage 05 packages pass.
- Stage 11c2's symlink-read test cannot create symlinks on this Windows account;
  run on Linux/CI. Daemon `TestStabilizeWorkingDirectoryChdirsToDataDir` also fails
  on the pre-09 archive: its TempDir cleanup precedes restoration of the working
  directory, so Windows refuses deletion. Correct cleanup ordering in stage 24.
- Latest fetched upstream is `795286c4e1a58a53269f687974c820cc10561b08`.
  Its two new commits change Claude auth readiness and tab UI, with no migration
  collision. They are assessed but not yet incorporated; current base remains
  `6d3ad8c7c`. Reconcile during the upstream integration pass.

## Definition of Done traceability

Numbers refer to the assignment's 48 Definition of Done items.

| DoD | Required behavior | Stage | Status |
| --- | --- | --- | --- |
| 1 | Manually create Agent Types in desktop | 06 | VALIDATED IN RUNNING APP |
| 2 | Different harness/provider/model per type | 08-09 | NOT STARTED |
| 3 | Supported custom provider options | 08 | NOT STARTED |
| 4 | Manually create Skills | 07 | VALIDATED IN RUNNING APP |
| 5 | Attach multiple pinned Skills | 07-09 | NOT STARTED |
| 6 | Configure and enforce manager ownership permissions | 05-06, 15 | IN PROGRESS: registry ownership plus native selection/transactional race enforcement TESTED; creation/version tool integration remains 15 |
| 7 | Manually launch from Agent Type | 09 | VALIDATED IN RUNNING APP |
| 8 | Explicit type selection when creating a task | 09-10 | TESTED in composer/CLI/API and native task dispatch adapters; desktop task graph integration remains 21/25 |
| 9 | Automatic Agent Manager selection | 15-16, 25 | IN PROGRESS: TESTED compatibility, immutable decisions, native automatic assessment/feedback and restart recovery (15a-c); shared scheduler consumption and live demonstration pending |
| 10 | Give orchestrator a high-level goal | 17 | NOT STARTED |
| 11 | Persistent orchestrator-created task dependencies | 10, 17 | IN PROGRESS: persistent DAG and orchestrator-authority storage tested; native orchestrator protocol remains 17 |
| 12 | Acceptance criteria frozen before work | 10 | TESTED: versioned planning, exclusive attempt pins, transactional context seal and native launch tests |
| 13 | Manager inspects available Types/Skills | 14-15, 25 | IN PROGRESS: versioned native Type/Skill reads, bounded candidate filtering, proposal reasons and retained deterministic decision evidence TESTED through 15c; live consumption remains 25 |
| 14 | Persistent Agent Manager service/controller/tools | TESTED | Durable native claims pushed as `c93ba87d7`. 14e3 wires the post-reconciliation inbox consumer, single native admission, fair durable scans, conservative recovery and generation-specific versioned CLI protocol. Exact input/delivery API/CLI history and telemetry redaction are exposed. Real SQLite consumer, native proposal race, boundary/fairness/unknown-start tests PASS; full storage/domain/service/CLI/spec/telemetry/skills, 40 frontend tests, build/typecheck/API/lint PASS. Full HTTP/daemon retain recorded Windows baselines. Semantic selection/evolution remains 15, uncertain controls/recovery 20/23 and live demonstration 25. |
| 15 | Manager composes existing Skills | 15 | NOT STARTED |
| 16 | Dynamic Skill creation | 15 | NOT STARTED |
| 17 | Dynamic Agent Type creation | 15 | NOT STARTED |
| 18 | Dynamic definitions visible in normal registry UI | 06-07, 15 | NOT STARTED |
| 19 | Concurrent heterogeneous workers | 09, 16, 25 | NOT STARTED |
| 20 | Preserve session/worktree/Git/PR/CI/review behavior | 09, 23-25 | NOT STARTED |
| 21 | Structured communication/handoffs | 12 | IN PROGRESS: typed results/API and durable message/delivery persistence tested; native message consumption remains 12c2 |
| 22 | Persist useful project knowledge | 11 | TESTED: versioned review/status/provenance, API/CLI and accepted context selection; desktop inspection remains 22/25 |
| 23 | Task-specific context with provenance | 11 | TESTED: bounded sealed context, native launch/restore and API/CLI; findings/contracts integration and live provider evidence remain 12/25 |
| 24 | Independently evaluate outcomes | 13 | TESTED: CI/test/build/lint, committed artifacts, mergeability and frozen native review evidence; current completion invalidation; live heterogeneous review remains 25 |
| 25 | Type/Skill/version performance | 13, 22 | IN PROGRESS: attempt evidence/usage, eight grouped dimensions, samples/exclusions and API/CLI tested through 13d2b; desktop views remain 22 |
| 26 | Manager routing outcomes | 18 | NOT STARTED |
| 27 | Orchestrator planning outcomes | 18 | NOT STARTED |
| 28 | Controlled experiments | 19 | NOT STARTED |
| 29 | Policy-controlled version evolution/recommendation | 19 | NOT STARTED |
| 30 | Inspect Type versions/performance | 06, 22 | NOT STARTED |
| 31 | Visual dependency inspection | 21 | NOT STARTED |
| 32 | Inspect manager decisions | 21 | NOT STARTED |
| 33 | Inspect/edit knowledge | 22 | NOT STARTED |
| 34 | Chronological audit | 22 | NOT STARTED |
| 35 | Needs Human items | 20, 22 | NOT STARTED |
| 36 | Unrelated branches continue around Needs Human | 16, 20 | NOT STARTED |
| 37 | Dry-run without worker/repository mutation | 20 | NOT STARTED |
| 38 | Pause project admissions | 20 | NOT STARTED |
| 39 | Resume safely | 20 | NOT STARTED |
| 40 | Drain active workers | 20 | NOT STARTED |
| 41 | Cancel pending/all | 20 | NOT STARTED |
| 42 | Enforce worker/profile/retry limits | 15-16 | NOT STARTED |
| 43 | Restart recovery | 23, 25 | NOT STARTED |
| 44 | Adaptive-off backwards compatibility | 23-25 | NOT STARTED |
| 45 | Relevant automated suites pass | 24 | NOT STARTED |
| 46 | Backend build passes | 24 | NOT STARTED |
| 47 | Desktop build passes | 24 | NOT STARTED |
| 48 | Major workflows actually exercised in app | 25 | NOT STARTED |

## Additional acceptance details to preserve

| Requirement | Verification needed | Status |
| --- | --- | --- |
| Type/Skill versions | Inspect, clone, diff, active-version rollback, disable, experimental/promotion history | NOT STARTED |
| Import/export | Round trip, schema rejection, unresolved bindings, no secrets or execution | NOT STARTED |
| Snapshot reproducibility | Project default edits and registry edits do not alter running/restored attempts | TESTED for original TUI/Chat configuration, retained resources and reopened SQLite; later execution-segment integration remains stage 09 |
| Ownership | Manager cannot spoof human origin or select/modify/version prohibited types | IN PROGRESS: TESTED 15a-c compatibility, decision transaction and automatic native assessment refuse protected Types/Skills and policy races; callers cannot forge selected resolutions or compatibility verdicts. Existing registry writes enforce independent policy. Creation-tool integration pending. |
| Context classification | Deterministically reject embedding technical < engagement < mission material above the attempt's pinned Agent Type clearance in delegation payloads, sealed manifests and knowledge selections; live Type edits cannot raise an existing attempt's clearance. Violation tests mirror Ownership. | TESTED foundation 15.0: exact-version authority, live activation, forged labels, required-task denial, SQL filtering before limit and atomic classified delegation seal. Manager input/proposal boundaries TESTED in 14e1-e3; compatibility and atomic automatic selection TESTED 15a-c. Native terminal feedback excludes unclassified caller reasons. Live-message/review integration pending. |
| Engagement isolation and upward flow | Per-client/per-target engagement context is shared only within its engagement; technical findings/results can flow into higher-class context. Test cross-engagement refusal and legal upward flow independently of clearance. | TESTED foundation 15.0: cross-engagement refusal even at mission clearance; technical results/contracts flow up; outputs inherit sealed sensitivity and worker knowledge candidates cannot lower or strip scope. Manager conversation binding/cumulative sensitivity across native generations TESTED in 14e1. Transport and remaining integration pending. |
| Classified provenance and authoring | Versioned inspectable delegation artifacts and sealed per-item classifications survive restart; Manager compatibility rejects tasks above clearance; registry, task composer and knowledge authoring expose selectors and session inspector displays retained classifications. | TESTED provenance foundation 15.0: per-item v2 labels, immutable numbered artifacts, exact prompt/hash/native pins, restart and rollback. Public artifact history, compatibility/authoring/inspector UI and live demonstration pending 15/25. |
| Criteria | Worker cannot lower criteria; revision retained and audited | TESTED: role denial, immutable revisions, attempt pins and context seal/readback |
| DAG safety | Cycle, cross-project edge, duplicate task/edge, cancellation, depth/pending bounds | NOT STARTED |
| Lease safety | Races, expiry, delayed heartbeat, unknown liveness, confirmed exit, retry exhaustion | NOT STARTED |
| Resource limits | Direct spawn, delegation, intake, restore and dynamic manager paths; runaway recursion | NOT STARTED |
| Review independence | Different implementing/reviewing type, optional different harness, exact target commit | TESTED: frozen policy, retained native configuration/launch and exact-result evaluation in 13c3; live heterogeneous review remains 25 |
| Evidence integrity | Pending/unknown checks, stale head, malformed results, self-reported tests, evaluator retries | TESTED: immutable evidence/retries, CI/artifact/mergeability/native review violations and current completion invalidation through 13d1 |
| Experiments | Comparable cohorts, sample sizes, policy-gated promotion and experiment bounds | NOT STARTED |
| Provider failure | Missing binary/model/auth/binding, deleted config, unsupported capabilities, provider crash | NOT STARTED |
| Recovery failures | Manager/orchestrator/evaluator crash, mid-launch restart, partial output, Git conflict | NOT STARTED |
| Controls | Concurrent launch vs pause/cancel, drain/stop distinction, partial termination failures | NOT STARTED |
| UI behavior | Forms, loading/errors/empty/disabled states, navigation, search, accessibility and restart persistence | NOT STARTED |
| Data isolation | Scratch lab state only; retained dirty worktrees; no credentials/build outputs in commits | NOT STARTED |

## Required loop for every milestone

1. Re-read the relevant source/contracts and update this plan if boundaries differ.
2. Implement one coherent vertical increment with domain/service/API/UI as needed.
3. Format, run narrow tests, then full relevant CI commands using pinned versions.
4. Inspect complete diff for duplication, races, restart windows, security and secrets.
5. Fix findings; rerun affected full suites; record exact results here or in validation.
6. Commit using a conventional message; push when fork access is available.
7. Perform running-app validation when the increment changes a user flow.
8. Update status based on evidence and continue; no feature is done from code alone.

## Final validation ledger to fill

- Upstream fetch/assessment and exact final incorporated SHA.
- Formatting, lint, complete backend tests/race/vet/build and generated drift.
- Full frontend and shared UI tests/typechecks; real desktop package build.
- Clean database and upgrade from the starting schema with data retention.
- Daemon/frontend restart and active-task/lease/provider-host recovery.
- Manual and manager creation, Skills, ownership, criteria and overrides.
- Heterogeneous configurations: name each actual harness/provider/model tested.
- Knowledge/context/results/messages/evaluation/performance/decision evidence.
- Experiments, limits, retry exhaustion and deliberate runaway-spawn attempts.
- Needs Human branch isolation, dry-run and all deterministic controls.
- Electron screenshots/interaction evidence from isolated data and checkout.
- Whole-diff architectural review, cleanup, documentation and final checklist.
- Commit history, fork/branch URL, push state and remote CI checks where available.
- Explicit implemented-but-not-live-tested list, remaining human actions and debt.
