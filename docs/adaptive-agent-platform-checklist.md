# Adaptive agent platform implementation checklist

Last updated: 2026-09-18. Starting upstream commit:
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
| 09 | Worker snapshots and manual Agent Type launch | IN PROGRESS | 09a `6f28a74fa`, 09b `036392a6c` pushed. Shared launch/restore, ordered resources, CLI/API, composer and launch inspector tested; 165 frontend tests and real idle native Codex Chat launch/reload pass. 09c1 adds migration 0152 and immutable prepared/activated execution history with atomic interface-change/rollback integration. Full domain/SQLite/registry suites, focused manager integration, backend build and changed-code lint PASS. Next: harness switches, live model settings and execution-history UI; then complete stage-09 running-app/restart/heterogeneous validation. Baseline Windows full-suite failures remain recorded. |
| 10 | Task DAG, revisions, criteria and leases | NOT STARTED | Transactional cycles/exclusivity; attempt-to-session seed association |
| 11 | Knowledge, bounded context and manifests | NOT STARTED | Candidate/accepted knowledge and file/source provenance |
| 12 | Worker result schema and typed communication | NOT STARTED | Ownership, bounded payloads, persistent idempotent delivery |
| 13 | Evaluator and attributable performance history | NOT STARTED | Objective evidence, target commits, sample sizes |
| 14 | Persistent Agent Manager service/controller/tools | NOT STARTED | Native harness execution; validated proposals and durable inbox |
| 15 | Automatic selection/composition/dynamic creation | NOT STARTED | Candidate rationale, permissions, approval gates, creation bounds |
| 16 | Shared scheduler and restart-safe dispatch | NOT STARTED | All launch callers; atomic limits/reservations; no blind reassignment |
| 17 | Orchestrator protocol and autonomous feedback loop | NOT STARTED | Durable plan/actions; bounded retries; completion against criteria |
| 18 | Manager/orchestrator outcome attribution | NOT STARTED | Routing/planning metrics distinct from worker metrics |
| 19 | Experiments, recommendations and evolution | NOT STARTED | Version diffs, comparable cohorts, explicit promotion policy |
| 20 | Dry-run, Needs Human and project controls | NOT STARTED | Deterministic behavior, independent DAG branches, race tests |
| 21 | Task graph and Agent Manager dashboard | NOT STARTED | Accessible graph/list, decisions and active worker population |
| 22 | Performance/knowledge/audit/control-center UI | NOT STARTED | Loading/error/empty/disabled states and navigation |
| 23 | Failure/recovery integration and upgrade matrix | NOT STARTED | Clean DB, existing DB, active-worker daemon/desktop restart |
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
| 6 | Configure and enforce manager ownership permissions | 05-06, 15 | NOT STARTED |
| 7 | Manually launch from Agent Type | 09 | VALIDATED IN RUNNING APP |
| 8 | Explicit type selection when creating a task | 09-10 | IN PROGRESS: composer/CLI/API exact pins tested; persistent task integration remains stage 10 |
| 9 | Automatic Agent Manager selection | 15 | NOT STARTED |
| 10 | Give orchestrator a high-level goal | 17 | NOT STARTED |
| 11 | Persistent orchestrator-created task dependencies | 10, 17 | NOT STARTED |
| 12 | Acceptance criteria frozen before work | 10 | NOT STARTED |
| 13 | Manager inspects available Types/Skills | 14-15 | NOT STARTED |
| 14 | Manager selects existing Types | 15 | NOT STARTED |
| 15 | Manager composes existing Skills | 15 | NOT STARTED |
| 16 | Dynamic Skill creation | 15 | NOT STARTED |
| 17 | Dynamic Agent Type creation | 15 | NOT STARTED |
| 18 | Dynamic definitions visible in normal registry UI | 06-07, 15 | NOT STARTED |
| 19 | Concurrent heterogeneous workers | 09, 16, 25 | NOT STARTED |
| 20 | Preserve session/worktree/Git/PR/CI/review behavior | 09, 23-25 | NOT STARTED |
| 21 | Structured communication/handoffs | 12 | NOT STARTED |
| 22 | Persist useful project knowledge | 11 | NOT STARTED |
| 23 | Task-specific context with provenance | 11 | NOT STARTED |
| 24 | Independently evaluate outcomes | 13 | NOT STARTED |
| 25 | Type/Skill/version performance | 13, 22 | NOT STARTED |
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
| Ownership | Manager cannot spoof human origin or select/modify/version prohibited types | NOT STARTED |
| Criteria | Worker cannot lower criteria; revision retained and audited | NOT STARTED |
| DAG safety | Cycle, cross-project edge, duplicate task/edge, cancellation, depth/pending bounds | NOT STARTED |
| Lease safety | Races, expiry, delayed heartbeat, unknown liveness, confirmed exit, retry exhaustion | NOT STARTED |
| Resource limits | Direct spawn, delegation, intake, restore and dynamic manager paths; runaway recursion | NOT STARTED |
| Review independence | Different implementing/reviewing type, optional different harness, exact target commit | NOT STARTED |
| Evidence integrity | Pending/unknown checks, stale head, malformed results, self-reported tests, evaluator retries | NOT STARTED |
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
