# Adaptive agent platform: environment and validation evidence

Recorded 2026-09-18. The latest milestone evidence below supersedes the historical
initial audit/environment failures retained later in this file. This is not the
final platform validation report.

## Stage 11c2 — bounded builder and native consumption (2026-09-19)

The shared builder seals input after actual workspace provisioning and before
native launch. It includes frozen task/criteria/dependency planning, versioned
parent planning, explicitly selected files, accepted relevant knowledge and exact
Type/Skill references. Knowledge is ranked by pin, task links, category and general
project relevance, capped at 32 candidates plus a truncation marker. Prompt bytes,
estimated tokens, per-source bytes and source counts are bounded; optional omissions
retain reasons. Required inputs that cannot fit prevent launch. Files use os.Root,
component symlink rejection, regular-file identity checks and bounded UTF-8 reads;
known credential paths are excluded. This path screen is not a secret scanner.

TUI and Chat launch consume the sealed prompt. Fresh TUI restoration reads the
original prompt despite changed files/knowledge/fallback metadata; native Chat
resume retains provider history without duplicating the initial turn. Missing
context requires reconciliation. Seal failure retains attempt/native reservations.

Five native context tests cover both interfaces, replay, budgets, frozen planning,
parent provenance and seal failure. A ranked/bounded knowledge-query test and file
screening tests PASS; symlink creation is SKIPPED (Windows privilege unavailable).
All focused context/task-native/store tests PASS (manager 0.984s, store 1.171s).
Full domain, SQLite (45.497s), store (20.382s), CDC, context/task services, Chat
(44.981s), session (56.648s), backend build and affected-package/changed-native
pinned lint PASS (0 issues). Full manager retains exactly the eight previously
recorded Windows baseline failures. Full daemon exposes a cwd/TempDir cleanup
failure, reproduced alone on current code and the pre-09 archive; it is not a
context regression. The test registers cwd restoration before TempDir cleanup,
which runs first and cannot delete Windows' current directory. Stage 24 must fix
that ordering. Logs: ignored `*stage11c2*`.

Budget fixtures were corrected to allow AO's real standing instructions before
testing optional-source headroom. A test-only import grouping lint issue was
corrected and lint rerun. These are recording native adapters, not live provider
validation. Previous findings/interface contracts integrate after typed results
in stage 12; context API/CLI inspection follows in 11c3, desktop inspection in 21/22.

## Stage 11c1 — sealed context persistence (2026-09-18)

Migration 0161 adds an immutable context manifest per attempt/session, linked to
the original configuration and pending native dispatch. Sealing validates exact
task/criteria content, pinned dependency revisions, all configured Type/Skill
references and current accepted same-project knowledge. Lease fencing,
cancellation and audit insertion share the transaction. Historical reads retain
exact content after source invalidation; sealing does not release native guards.
Optional explicit task file selections preserve historical definition hashes.

Four store tests cover replay/reopen, forged evidence and ownership, late
invalidation/cancellation, atomic audit failure and SQL immutability. Four domain
tests cover prompt integrity, portable confined paths, source-data rendering and
historical hashes, with additional task-bound cases. Full domain, SQLite,
store, CDC, task-service and API-spec suites PASS (SQLite 38.846s, store 17.229s,
specgen 11.387s). Final domain rerun PASS (0.485s). Backend build and full lint of
domain/ports/SQLite PASS (0 issues). sqlc, API generation and frontend typecheck
PASS. Logs are ignored
`*stage11c1*`. Native context construction and consumption remain the next slice;
these persistence tests are not live worker evidence.

## Stage 11b — knowledge service, API and CLI (2026-09-18)

The daemon mounts six shared knowledge routes for project search/create, current
inspection, revision and exact/paged history. Actor authority is assigned by the
daemon, with strict bounded single-object JSON. API errors preserve operational
failures, ownership denial and optimistic conflicts. `ao knowledge` exposes the
same six operations through HTTP, with bounded file/stdin requests and literal
search/status/kind filters. Generated contracts, route telemetry templates and
CLI documentation are updated. Soft-deleted claims remain explicitly readable.

Three HTTP integration tests, two CLI tests and a service error-boundary test
PASS, including full API mounting alongside existing project routes. Full
knowledge, CLI (23.79s), telemetry, router/envelope and API-spec suites PASS.
Full controllers reproduce exactly the known Windows mobile rename-access and
file-URL clone failures. Backend build, frontend typecheck, 39 API client tests,
changed-code pinned lint (0 issues) and whitespace/source review PASS. Semantic
OpenAPI comparison shows zero changes to any prior route/schema. A new frontend
test initially passed a URL query to the pathname-only normalizer; corrected the
test to its existing caller contract and reran the complete client suite. No
unrelated normalizer behavior was changed. Logs: ignored `*stage11b*`.
Knowledge context construction and native injection remain the next slice;
the management UI and its live interaction checks remain stage 22/25.

## Stage 11a — versioned project knowledge persistence (2026-09-18)

Migrations 0159–0160 add project-scoped knowledge identities, immutable hashed
versions and trigger-driven CDC. Versions retain title/kind/content, confidence,
review disposition, pins, task/tag relevance and exact source identities/hashes.
Only accepted knowledge may be pinned. Supersession references an accepted exact
version in the same project. Deletion withdraws default selection while retaining
historical content. Bounded literal search filters current status/kind/content.
Worker/orchestrator creation is candidate-only; workers must establish their own
active attempt/session provenance, and cannot accept or rewrite knowledge.
Trusted human/system review is explicit and fenced by expected version. Native
Agent Manager proposal authority is not invented before its stage 14 identity.

Domain bounds plus five store tests and populated migration upgrade/downgrade
PASS. They cover retained history/restart, filtered deletion, source/authority
and project fences, concurrent review CAS, CDC rollback and immutable history.
The first focused run exposed mixed numbered/unnumbered SQL parameters in the
search query; changed the query source, regenerated sqlc and reran successfully.
Full domain, SQLite (37.74s), store (18.71s), sqlitetest and CDC suites PASS, as
does the backend build. Full affected-package pinned lint PASS (0 issues) after
adding the required exported-error documentation. Source/generated/whitespace
diffs reviewed. Logs: ignored
`*stage11a*`. Service/API, context manifests and UI are subsequent stage 11/22
slices; no claim is made that knowledge is already injected into workers.

## Stage 10e — audited intent and derived task state (2026-09-18)

Migration 0158 retains immutable run/cancel instructions with actor/reason,
planning revision and a separate control-version fence. Store transactions block
new reservations, worker seeds and native operations for cancelled tasks or
ancestors. Cancellation never releases ownership or resolves unknown native
side effects. Existing guards can still be reconciled and leases released after
verified termination. Current task views derive planned/blocked/ready/leased/
working/retry-exhausted/cancelling/cancelled state from facts; pending execution
operations are inspectable without exposing holder tokens or internal owners.
Two new HTTP routes and `ao task intents`/`set-intent` share the task service.
Generated OpenAPI/TypeScript and CLI documentation updated together.

Five intent-store tests PASS for restart/history, descendants/unrelated work,
seed/restore fences, authority/concurrent CAS, retained uncertain execution and
atomic audit failure. Two service tests, new HTTP intent test, expanded CLI
tests and native task-worker regression PASS. Full domain, SQLite (50.00s), store
(26.04s), CDC, task service, CLI (29.61s), telemetry, API-spec and route/envelope
suites PASS. The full SQLite suite first found the missing migration-ledger
entry; fixed and the full suite rerun. Full HTTP controllers reproduce only the
recorded mobile rename-access and Windows file-URL clone baseline failures.
Backend build, frontend typecheck, 38 API client tests, generated SQL/API drift,
changed-code pinned lint (0 issues) and whitespace/source review PASS.
Logs: ignored `*stage10e*`.

Stage 10 foundation is TESTED, not a claim of autonomous dispatch or platform
completion. Verified result/review/completion state follows in stages 12/13;
all-entry-point admission, automatic recovery, process cancellation cleanup and
real graph workflows remain stages 16/20/21/23/25. No user-facing stop action is
reported successful from the cancellation instruction alone.

## Stage 10d2 — task worker native launch and restore (2026-09-18)

The trusted manager dispatch path now creates the immutable worker snapshot and
attempt/session association atomically, reserves the native target generation
before workspace/process side effects, and passes it to the existing TUI
supervisor or Chat controller. Successful connection resolves the guard. Replay
returns the retained session without a new readiness check or process, including
after a lost launch response. Uncertain failures retain the guard and exclusive
lease. Restore uses the same lease/guard before workspace recovery; released
historical task workers cannot restart. Legacy sessions retain their launch path.

Four focused task-worker tests (including both TUI and Chat) PASS (0.57s), as do
the configured-worker regression suite (0.68s), full Chat (36.78s), full session
service (49.53s), backend build and changed-code pinned lint (0 issues). Full
session manager (41.66s) reports exactly the eight previously isolated Windows
baseline failures: three source-handoff path checks, handoff POSIX mode, interface
transition lookup error, dev namespace and two executable/node PATH cases.
Source/diff reviewed, including initial-prompt delivery under the manager's
exclusive operation. Logs: ignored `*stage10d2*`. Shared scheduler admission,
automatic uncertain-operation reconciliation and the broader native/restart
matrix remain stages 16/23/25; this is not a claim that stage 10 is complete.

## Stage 10d1 — durable native execution reservations (2026-09-18)

Migration 0157 adds immutable task execution operations and verified lifecycle
resolutions. Unresolved native side effects block lease release and holder
transfer in both store transactions and SQLite triggers. A released task worker
cannot be resurrected by a legacy session update; restoration requires its
unreleased lease and pending execution reservation. Idempotent operation replay
returns `created=false`, never permission to launch twice. The operation ID is
reserved for the native target generation so later recovery can identify a host
created before its final session metadata was committed.

Four new tests PASS for retained unresolved execution across restart, stale
proof rejection, idempotent resolution, restore/release races (one winner), SQL
resurrection/release fences and fault-injected resolution/audit/CDC rollback.
Populated migration upgrade/downgrade now includes an unresolved native operation.
Full domain, SQLite (34.41s), store (16.54s), sqlitetest and CDC suites PASS; backend
build, sqlc generation and full affected-package pinned lint PASS (0 issues).
Source/diff reviewed. Logs: ignored `*stage10d*`. This verified persistence slice
prepares the native integration; the manager does not yet consume these guards.

## Stage 10c — shared task service, HTTP and CLI (2026-09-18)

The daemon now mounts ten task authoring/history routes through a shared service:
project-scoped list/create, current planning, exact/paged revisions, criteria
revisions/exact reads, audit and attempts. Inputs exclude actor/lease ownership;
human identity is assigned server-side and trusted controller calls retain store
authority checks. Manual Type references must name an existing Type/version.
Historical work and exact frozen criteria are separate from current lease facts.
API responses exclude scheduler holder tokens. OpenAPI and TypeScript contracts
are regenerated together. Explicit nullable-array schema tags preserve historical
definition hashes without lying about the JSON wire representation. A semantic
comparison confirms every previously existing route/schema is unchanged.

`ao task` uses HTTP exclusively for all ten operations. JSON file/stdin bodies are
bounded to 256 KiB; page/version/missing argument misuse exits 2, while daemon
errors retain their machine code/request ID and exit 1. CLI docs include a usable
task/criteria example. Renderer telemetry templates redact task/project IDs.

Seven task API integration tests, two service tests, task schema regression and
three CLI tests PASS. Full domain, task service, HTTP router/envelope/API-spec,
CLI (16.44s) and telemetry suites PASS. Full controllers reproduce only the two
previous Windows baseline failures (mobile pairing rename and file-URL clone);
new task routes also pass mounted alongside the full daemon API. Full CLI first
caught missing command telemetry classification; corrected and rerun completely.
Frontend typecheck, 38 client tests, backend build and changed-code pinned lint
PASS (0 issues). Generated/source/whitespace diff reviewed. Logs: ignored
`*stage10c*`. Native task launch/restore integration remains the next stage 10 slice;
these authoring APIs do not bypass pending shared scheduler admission.

## Stage 10b — exclusive leases and atomic dispatch persistence (2026-09-18)

Migration 0156 retains immutable attempts and launch intent IDs, exact task/
criteria/dependency revision pins, one unreleased lease per task, independent
heartbeat/activity timestamps, and immutable attempt-to-session associations.
Expiry preserves the reservation. Explicit recovery requires the exact session
identity and controller owner observed by the trusted lifecycle caller; release
requires termination, while adoption changes the holder and fences old callbacks.
Unseeded cancellation is safe because process launch requires a committed seed.
Releasing ownership does not mark the task successful. Attempt limits are enforced.

The shared configured-session transaction now also supports atomic task dispatch:
session seed, validated immutable worker configuration, association and audit/CDC
commit together. Replayed or concurrent dispatch returns the same seed with
`created=false`, never fresh-launch authorization. A retained dispatch prevents
deleting its worker identity. Requested manual Type/override selection is enforced.

Nine new lease/dispatch tests PASS for frozen history/reopen, unfrozen rejection,
retry bounds, independent activity, stale/expired owners, six concurrent
reservations, idempotent launch intent, five concurrent seeds, association/audit
fault rollback, exact recovery/termination proofs, retained SQL history and
manual selection. The populated upgrade/downgrade test now includes leases.
Full domain/SQLite (36.96s)/store (18.42s)/sqlitetest/CDC suites PASS; backend build,
sqlc and pinned domain/ports/SQLite/CDC lint PASS (0 issues). Diff reviewed.
Logs: ignored `*stage10b*`. These are internal persistence contracts; scheduler
admission, native launch/restore integration and task service/API remain pending,
so stage 10 remains IN PROGRESS and no autonomous execution claim is made.

## Stage 10a — durable task planning and criteria (2026-09-18)

Migrations 0154–0155 add task CDC and project-scoped task identities, immutable
planning/criteria revisions, bounded parent/dependency graphs and append-only
audit. One transaction validates the entire affected graph, advances its revision
with optimistic concurrency, replaces active edges and emits trigger CDC. Workers
and Agent Manager cannot change planning or criteria; orchestrator mutations
require a live orchestrator session in the same project. Historical criteria and
requested worker selections remain attached to their exact planning revisions.

Ten focused top-level tests PASS for input bounds, revision and criteria history,
scope/authority, cycles/cross-project references, descendant depth, concurrent
opposite edges, fault-injected rollback, retained-history SQL protections,
pagination, reopen and migration upgrade/downgrade. The first run found that
`project_updated` was not a permitted CDC event; the corrected additive vocabulary
uses `adaptive_task_changed`, preserving existing events and triggers. Project
deletion tests honor the existing CDC retention foreign key before cascading.

Complete domain (0.95s), SQLite (32.57s), store (13.29s), sqlitetest and CDC suites
PASS. Pinned golangci-lint over domain/ports/SQLite/CDC PASS (0 issues), including
vet; backend build and sqlc generation PASS. Diff/whitespace reviewed. Logs are
ignored `*stage10a*` files. This milestone implements task persistence only: leases,
attempt-to-session dispatch association and task service/API remain stage 10.

## Stage 09c5 — recoverable native controls; stage 09 tested (2026-09-18)

Migration 0153 records immutable native-control intent before provider I/O and
immutable applied/reverted resolutions. Applied resolution, native preferences,
execution snapshot and activation share one transaction. Requests validate the
live advertised choices and shared native catalog/binding before mutation. The
returned coupled controls are retained as bounded values, without copying native
credentials/config files. A model change can legitimately change its effort
catalog; AO validates and records the provider's confirmed result.

Cancellation or persistence failure attempts bounded readback-verified
compensation. Unknown compensation remains durable, blocks dispatch/retry and
configuration ownership transfer, preserves queued work, and appears in the
inspector. Retrying the native control or restarting reconciles it before further
work. Successful recovery resumes accepted queued messages. Fresh controllers
restore retained native controls; pending harness targets receive their prepared
controls so the source cannot overwrite them before activation. Explicit portable
configuration changes and TUI transitions reset interface-specific controls in a
new execution record. Ordinary live-host adoption does not reapply controls.

Real-SQL/fake-native tests PASS for validation before side effects, coupled model/
effort and boolean controls, exact restore, prepared-target defaults, immutable
intent/resolution, overlapping intent rejection, foreign recovery, fault-injected
resolution rollback, confirmed compensation, unknown outcome dispatch fencing,
retry/restart recovery, cancellation and resuming the accepted queue. Complete
domain, registry, store, SQLite upgrade/migration (37.37s), Chat (final 32.47s),
and session service (57.47s) suites PASS. The full manager suite (44.83s) reproduces
only the same eight documented Windows baseline failures; its focused worker
tests PASS. Migration-ledger omission caught by the first full run was fixed and
the complete affected SQLite suite rerun successfully. Complete API-spec suites,
sqlc/API generation, build, affected vet, frontend typecheck, 40 frontend tests
and pinned changed-code lint PASS (0 issues). Diff and whitespace reviewed.

Stage 09 implementation is tested. The prior increment's native Codex launch,
turn, settings history and full desktop/daemon restart remain the live evidence.
This native-controls recovery increment uses injected protocol/SQLite evidence;
live Claude/OpenCode/custom-provider combinations and the empty-native-thread
recovery gap remain explicitly in the stage 23/25 matrix, not claimed passed.
Logs: ignored `*stage09c5*` files. The live lab remains on the 09c4 build.

## Stage 09c4 — execution history API/UI and native turn (2026-09-18)

Two session-scoped read routes expose the active configuration, bounded
chronological activation summaries, and lazily requested exact change content.
Rollback summaries show the restored destination while preserving the original
operation's actor/reason. Pages exclude activations newer than the configuration
read; cursor/limit bounds and cross-session reads are checked. The inspector
separates immutable launch facts from current configuration and later changes,
with loading, empty, error/retry, pagination and retained-content disclosure.
Trigger CDC and reconnect invalidate its history cache.

Service/API tests PASS for rollback destinations, pagination, concurrent reads,
invalid cursors/limits, absent/legacy sessions and cross-session isolation. All
75 focused frontend tests (four files) PASS. Complete domain, session service
(55.97s), HTTP router/envelope and API-spec suites PASS. The full controller suite
reproduces only the two previously documented Windows baseline failures (mobile
pairing rename and file-URL clone). Backend/lab-daemon builds, affected vet,
frontend typecheck and pinned changed-code lint PASS (0 issues). Generated
contracts and complete source/whitespace diff reviewed.

The existing isolated Electron lab was refreshed without reinstalling or using
real AO data. The original never-prompted Codex thread (`standalone-1`) reported
`no rollout found` on resume after its host stopped; preserve this empty-native-
thread recovery case for stage 23 rather than claiming a successful resume.
A second Type-v3/Skill-v1 Codex Chat worker (`standalone-2`) launched with 201.
Using the real effort picker to select Low returned 200, created execution
`c1d817db-5517-402d-8b35-dc5cf2589974`, and refreshed the inspector through CDC.
The provider answered the bounded, no-tools prompt with `adaptive-worker-ready`.
Lazy details and renderer reload passed. Killing only the isolated Electron main
and daemon (leaving its native chat host alive), then restarting both, retained
the reply, original configuration, execution event and Low setting; the worker
reconnected with no disconnected-controller banner. Screenshots were inspected:
`electron-execution-changed.png`, `electron-execution-restarted.png` (ignored).

Provider-owned live controls outside the per-turn settings endpoint and broader
heterogeneous validation still remain; stage 09 is not marked complete.
Evidence logs/scripts: ignored `*stage09c4*` and `*execution*` files.

## Stage 09c3 — durable per-turn settings attribution (2026-09-18)

Configured Chat workers validate model, effort, permissions and native binding
through the shared registry resolver before changing next-turn preferences.
Preferences and their immutable execution activation now commit in one database
transaction, fenced by controller generation, controller ownership and previous
configuration sequence. The original launch remains unchanged. Restoration uses
the latest recorded permissions; a later harness change drops provider-owned
settings from its predecessor. Legacy sessions retain their existing settings path.

New real-SQL tests PASS for unsupported native settings, unchanged controller on
rejection, stale controller/configuration fences, cross-conversation rejection,
fault-injected settings/history/CDC rollback, duplicate suppression, and database
reopen. Full domain, registry (1.10s), store (9.03s), and Chat (42.61s) suites PASS;
the earlier intermittent Chat baseline failure did not reproduce in this run.
Focused configured-worker manager tests, complete API-spec suites, backend build,
affected-package vet, frontend typecheck and changed-code pinned lint PASS
(0 issues). API/schema regenerated together; source and whitespace diff reviewed.
Whole-repository lint again cannot typecheck the pre-existing Windows
`persistenthost/host_race_test.go` uses of `syscall.Kill`; this is not a full lint
pass. Logs: ignored `*stage09c3*` files.

Native provider-owned live controls and execution-history UI remain next; this
increment covers the per-turn settings endpoint, not those separate controls.
No new live provider turn has been claimed. Stage 09 remains in progress.

## Stage 09c2 — harness-switch execution configurations (2026-09-18)

TUI and Chat harness switches prepare a validated execution configuration before
source teardown and activate it in the existing ownership-transfer transaction.
They retain frozen Type/Skill content and standing project instructions. Selecting
a different harness explicitly records clearing the previous harness's provider
reference; it cannot silently transplant that binding or incompatible model/
effort aliases. Target-native defaults and requested models pass the same native
validation. Same-harness changes never re-inherit edited project defaults. Failed
Chat switch recovery uses the retained source configuration.

Tests PASS for explicit provider rebinding, disabled original Type retention,
cleared options, immutable source content, native target-preparation arguments,
and fault-injected atomic ownership/configuration activation for both TUI and
Chat. Full registry (2.60s) and SQLite store (14.42s) suites PASS. The full manager
suite (39.91s) reproduces exactly the eight already confirmed Windows baseline
failures listed under 09b; no additional failure appeared. Backend build and
pinned changed-code lint against `fcd09f60e` PASS (0 issues). Complete pending
diff and whitespace reviewed. Logs: ignored `*stage09c2*` files.

This increment has automated evidence; native multi-harness switching and its
history UI are not yet live-validated. Live model settings/history presentation
remain in progress, so stage 09 remains open.

## Stage 09c1 — execution history and interface changes (2026-09-18)

Migration 0152 adds immutable execution configurations and append-only activation
events. Preparation records its source action, trusted actor, reason, current
controller owner and prior activation sequence. It does not change the active
configuration. Applying an interface change and its activation event share the
existing controller-epoch transaction; rollback restores the recorded predecessor
and appends a rollback event. CDC remains trigger-owned. Restoration reads the
last activated configuration, while the original launch record stays unchanged.

Focused store/manager tests PASS for idempotency, conflicting retry payloads,
stale owners, cross-session reads, bounded history, immutable SQL records,
unprepared mode rejection, injected activation failure with mode/CDC rollback,
reopened-database history, retained-session deletion protection, and a TUI-to-Chat
manager preflight/launch retaining its original model, effort and instructions.
The complete domain, SQLite (35.69s), SQLite store (11.74s), sqlitetest and registry
suites PASS. Backend build PASS. Pinned changed-code lint against `036392a6c`
PASS with 0 issues after adding exported-method documentation and removing an
unnecessary conversion. sqlc regenerated from the new migration/queries; diff
whitespace and the complete source/transaction changes were inspected.

The new execution-history behavior has automated coverage only at this milestone.
Harness switches, live conversation-setting changes and the history UI remain
the next increment; stage 09 is not yet complete. The existing Electron lab still
runs the verified 09b build. Logs are ignored `*stage09c*` files.

## Stage 09b — manual launch and retained configuration (2026-09-18)

Agent Type selection now reaches the existing shared session manager from direct
HTTP/CLI spawn and task delegation. It resolves exact versions, compatible project
defaults and explicit one-off overrides, checks fresh native prerequisites, and
atomically persists the sealed snapshot with the existing session seed. Ordered
Skill instructions/resources are materialized beneath the isolated AO data root
using confined filesystem operations, without editing native user Skill folders.
Existing resource content must match; changed content is rejected, not overwritten.
Restoration uses retained instructions/options, including explicitly empty values.
Chat host adoption skips fresh-process validation; fresh controllers require it.

The desktop composer supports exact Type versions, native model/mode/permission
choices, one-off instructions and temporary Skill composition. Registry launch
opens that same composer. The inspector reads historical launch facts without
consulting current Type availability. CLI flags are `--agent-type`,
`--type-version` and `--worker-overrides`; mixed legacy configuration is rejected.
The advisory registry check now resolves the same project/interface defaults as
launch. Generated OpenAPI and frontend types include selection and history.

| Check | Result |
| --- | --- |
| Worker/registry/provider/CLI/API/spec focused tests | PASS; exact pins, explicit clearing, trusted actor, legacy-null history, errors/request IDs, resource retention, rejected unavailable launch, TUI/Chat restore across SQLite reopen, and Chat adoption vs fresh launch |
| Full `service/registry`, `service/session`, `cli`, HTTP router/spec/specgen/envelope suites | PASS; service/session 47.02s, CLI 19.05s |
| Full `session_manager` | FAIL: eight existing Windows cases listed below; new worker tests PASS |
| Full HTTP controllers | FAIL: existing concurrent `mobile.json` rename access denial and Windows clone file-URL rejection, recorded in prior milestones |
| Six frontend files (selection, inspector, registry launch, composer, registry, session inspector) | PASS, 165 tests, 48.98s |
| `npm run frontend:typecheck` | PASS |
| Pinned golangci-lint, touched packages, `--new-from-rev=6f28a74fa` | PASS, 0 issues |
| `go build ./...`; relevant `go vet` | PASS |
| Renderer Vite build; isolated lab daemon build | PASS; existing renderer chunk-size warning |
| `git diff --check`; complete pending source/contract diff review | PASS |
| Real Electron manual Type launch, retained configuration, reload | PASS for an idle native Codex Chat worker; no model turn/completion claimed |

The full manager failures are `TestBuildSourceHandoffRequestUsesCurrentNativeSessionContext`,
`TestSwitchAgentFreshPreservesAOIdentityAndDeliversArtifact`,
`TestSwitchAgentRefreshesLateSourceNativeIdentityAtStopBoundary`,
`TestWriteAgentHandoffFileIsPrivateAtomicAndImmutable`,
`TestInterfaceTransitionReservedTranscriptRequiresUntouchedTerminal/lookup_error`,
`TestSpawn_DefaultsBranchUnderDevNamespaceForDevDataDir`,
`TestSpawnAndRestore_PrependsResolvedBinaryAndNodeDirsToRuntimePATH`, and
`TestSpawn_DoesNotAddNodeRuntimeForNativeBinary`. The last three were independently
reproduced against stage-08 sources. All first five also reproduced against an
isolated archive of pre-change commit `6f28a74fa` (`GOWORK=off`), confirming they
precede 09b. They involve Windows path escaping, Unix permission expectations
and the Windows non-directory lookup result. Full-suite acceptance remains
outstanding; none of these failures is labeled passed.

Reused the isolated Electron worktree/data and native Codex provider binding.
The UI launched Type `Lab Codex reviewer` v3 with Skill `Lab review evidence` v1,
one-off Chat mode and bounded validation instructions, creating `standalone-1`.
Native Chat connected and displayed idle with its native default model picker.
The inspector displayed exact versions, binding and USER provenance, and retained
the one-off override after renderer reload. Screenshots
`electron-worker-selection.png` and `electron-worker-history.png` were captured
and visually inspected. No worker prompt, paid model completion, repository
change, or heterogeneous live execution is claimed by this check.

Stage 09 remains IN PROGRESS. Later harness/interface/model changes still need
immutable execution segments before the complete restore feature is accepted.
The current guard rejects a fresh restore whose harness/interface differs from
the original snapshot rather than silently restoring incompatible defaults.
Relevant logs/scripts/screenshots remain ignored under `.cache/adaptive-tests`.

## Stage 09a — atomic worker snapshot persistence (2026-09-18)

The first stage-09 milestone adds sealed schema-v1 configuration records,
explicit pointer-valued one-off overrides, exact Type/Skill references and
content, and migration 0151. `CreateConfiguredSession` reuses the existing
session identity allocator and commits the seed plus snapshot in one transaction.
It rechecks Type/Skill enabled state, manager selection policy, immutable version
hashes and provider binding revision/scope while holding the write transaction.
The existing unconfigured session path retains its behavior.

Full `go test ./internal/domain ./internal/storage/sqlite/...` passed (SQLite
30.74s, store 6.78s). Pinned golangci-lint on domain/ports/SQLite passed with
0 issues after fixing one import grouping. Fault injection proves failed
snapshot insertion rolls back the session and CDC. Tests also prove database
immutability, original content/name retention after disable and store reopen,
manager permission revocation, human selection, hash tampering rejection and
safe cascade during existing seed rollback. An initial reopen test incorrectly
used the empty-database clone helper; it was corrected to `sqlite.Open` and the
full suites rerun. Logs: ignored `backend-stage09a.log`, `lint-stage09a.log`.
Stage 09 remains IN PROGRESS until launch/restore and UI are connected and tested.

## Stage 08 — native capabilities and provider references (2026-09-18)

Added adapter-declared configuration fields, bounded Chat capability inspection,
native model selection, permission controls and shared native sign-in navigation.
Provider bindings contain only a native provider reference, optional project
scope and descriptive state. Migration 0150 preserves immutable targets and
audited revision-checked label/availability changes. Credentials and arbitrary
endpoint configuration are not accepted. Required missing bindings never fall
back to defaults. All mutation provenance comes from the human controller.

The shared configuration check distinguishes invalid options from unavailable
native prerequisites. Fresh worker launch will resolve project defaults and
one-off overrides before this check in stage 09; this milestone exposes advisory
authoring checks. Unknown Skill tool/MCP requirements remain explicitly unverified
and cannot silently grant capabilities. Active-worker adoption is not a fresh
launch and must retain its snapshot in stage 09.

| Command/check | Result |
| --- | --- |
| `npm run sqlc`, `npm run api` | PASS; migration/query and code-first API artifacts regenerated |
| `go test ./internal/service/registry ./internal/domain ./internal/storage/sqlite/... ./internal/adapters/agent/codex ./internal/httpd/apispec/...` | PASS; full listed suites, SQLite 31.71s, store 8.75s |
| Configuration/registry/provider/migration/spec focused tests across service/agent, HTTP and SQLite | PASS; strict credential-field rejection, API error envelopes, disabled/missing binding, native scope/model/effort/capability validation and audit/CDC covered |
| Four focused frontend files: binding editor/checks, configuration editor, registry and API client | PASS, 46 tests, 7.64s |
| `npm run frontend:typecheck` | PASS |
| `go build ./...`; touched service/SQLite/HTTP `go vet` | PASS |
| Pinned golangci-lint over touched packages | 12 existing findings in unchanged `codex_secure_fs_windows.go`; exact categories 5 errcheck, 2 gocritic, 3 gosec, 2 staticcheck |
| Same pinned lint with `--new-from-rev=ac7e7cc26` | PASS, 0 new issues |
| `npx vite build --config vite.renderer.config.ts` | PASS, 2.53s; existing chunk-size warning. Initial command used nonexistent `.mts` suffix, corrected and rerun |
| Real isolated Electron + rebuilt Go daemon | PASS for binding/editor/validation flow below |

Earlier stage-08 full service/agent tests exposed existing Windows ACL ancestor,
symlink-privilege and model-cache timing failures. Full service/chat exposed a
duplicate conversation-message ID in the existing completed-Codex handoff test.
These are not labeled passed; they join the recorded platform/CI follow-ups.
Logs are retained under ignored `.cache/adaptive-tests/*stage08*`.

The existing isolated worktree/data were reused without dependency reinstallation.
The Go daemon was rebuilt and Electron restarted on 3036 with CDP 9336. The real
UI created `Lab native Codex`, saved and activated Agent Type v3 using the binding,
disabled that binding and observed validation rejection, re-enabled it and
observed native installed/authorized readiness, then reloaded and verified the
version persisted. The initial script expected unavailable native auth; actual
fresh readiness was ready, so verification was corrected to test the observed
state plus an explicitly disabled binding. No worker was launched. Screenshots
`electron-provider-binding.png`, `electron-provider-unavailable.png` and
`electron-provider-readiness.png` were captured and visually inspected. Custom
native provider/scope cases have injected-catalog coverage; live custom-provider
worker execution remains a later validation requirement.

## Stage 05 — registry persistence (2026-09-18)

- Resumed in the user-provided full-access session. `git fetch upstream` passed;
  upstream main is `795286c4e1a58a53269f687974c820cc10561b08`. Its migrations still
  end at 0147. No upstream merge/rebase performed in this milestone.
- Added migrations 0148 (additive CDC event vocabulary) and 0149 (registry,
  immutable versions, ordered kind-safe Skill pins and durable audit), plus the
  migration ledger entries. No previously merged migration was edited.
- Agent Type/Skill content has structural limits and typed AO harness/config
  fields. Skill resources reject traversal, Windows device names, control
  characters, case-fold collisions and attempts to replace the root SKILL.md.
- Store writes validate trusted actor provenance, enforce manager modification
  and versioning policies inside the transaction, use optimistic revisions,
  retain old versions on activation/rollback, and commit audit plus trigger-owned
  CDC atomically. Activation audit retains the exact target version.

| Command (backend directory unless noted) | Final result |
| --- | --- |
| Root `npm.cmd run sqlc` | PASS, pinned sqlc v1.31.1; generated artifacts committed with sources |
| `gofmt -w` on changed Go files | PASS |
| `go test ./internal/domain ./internal/storage/sqlite/... ./internal/cdc` | PASS; final SQLite 27.671s, store 4.711s, sqlitetest 0.460s, CDC 0.489s; domain 0.533s |
| `go vet ./internal/domain ./internal/storage/sqlite/... ./internal/cdc` | PASS |
| `go build ./...` | PASS |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --path-mode=abs ./internal/domain/... ./internal/ports/... ./internal/storage/sqlite/... ./internal/cdc/...` | PASS, 0 issues |
| `go test -race ./internal/domain ./internal/storage/sqlite/... ./internal/cdc` | NOT RUN: cgo disabled; retry with `CGO_ENABLED=1` failed to compile because `gcc` is unavailable |
| `git diff --cached --check` | PASS |

Eleven new top-level tests cover invalid definitions/actors/resources, immutable
version content/hashes, exact Skill pins after Skill version activation, manager
ownership, forbidden policy escalation/promotion, twelve concurrent writers at
one revision, missing-pin rollback, injected audit failure rollback, clean DB,
populated 0147 DB upgrade with existing session/CDC preservation, database-level
history constraints, and close/reopen persistence.

Initial validation failures were fixed: new migrations needed ledger entries;
the upgrade test initially changed `latest_user_prompt`, which intentionally does
not emit session CDC, and now changes `activity_state`. Diff review added exact
activation target provenance to audit. All affected full suites were rerun.

The first full pinned lint run failed on existing Windows-only code: the
persistenthost race test uses unavailable `syscall.Kill`; process helpers report
unchecked CloseHandle, wrapped-error comparison, narrowing PID conversions and
unsafe-pointer findings. Checkout CRLF also caused goimports diagnostics; Go
working-copy line endings were normalized to LF without changing Git content.
Full-repository Windows lint remains a stage 24 gap; it is not labeled passed.

No registry API/UI, worker integration, real Electron flow, live provider call,
or complete adaptive Definition of Done has been validated at stage 05.

## Stage 06 — registry API, CLI and desktop authoring (2026-09-18)

The daemon now exposes the shared registry service through 20 human authoring
routes and HTTP-only `ao agent-type` / `ao skill` commands. Actor provenance is
established by the controller; JSON role/origin spoofing is rejected. Versions
are appended inactive and activation/rollback requires the current revision.
Manager-created records use the same service and appear in the same API/UI.

| Command / check | Result |
| --- | --- |
| Registry controller tests | PASS, 4 top-level tests including full authoring lifecycle, strict input, manager policy and unavailable service |
| `go test ./internal/cli ./internal/telemetrymeta` | PASS, 13.165s / 0.015s; includes new commands, usage errors and preserved daemon error envelopes |
| Full domain, ports, SQLite, CDC, API/spec/specgen/envelope and skillassets suites | PASS |
| Full HTTP controllers suite | FAIL on two unrelated Windows tests: concurrent mobile.json rename access denied, and project clone file-URL validation; registry tests pass |
| Pinned golangci-lint v2.12.2 on domain/ports/storage/CDC/registry service/HTTP/CLI/telemetry packages | PASS, 0 issues |
| API generation | PASS; specgen then pinned openapi-typescript 7.4.4. Root dependencies restored for the normal `npm run api` command |
| Canonical generated OpenAPI comparison | 14 new path groups, 19 new schemas, zero semantic changes to existing paths/schemas; generated ordering accounts for the larger textual diff |
| `npm.cmd run frontend:typecheck` | PASS |
| Focused registry/event transport/API client/sidebar tests | PASS, 174 tests; registry rerun after accessible label fixes: 4 PASS |
| ShellTopbar tests after registry title fix | PASS, 40 tests |
| `npx vite build --config vite.renderer.config.ts` | PASS, 2.76s; route tree generated |
| Full `npm test -- --maxWorkers=2` in frontend | FAIL: 302 files passed, 22 failed; 4,718 tests passed, 169 failed, 8 skipped. Failures include macOS path/signing/update assumptions, Windows file/socket behavior, missing native SQLite binding and landing dependencies. Exact diagnostic log retained locally; stage 24 must resolve or verify in the supported CI environment |
| `git diff --cached --check` | PASS |

Actual Electron validation used the `ao-desktop-dev` skill, detached isolated
worktree `.cache/ao-adaptive-lab`, real npm installs in each workspace, and
scratch data/profile/runfile under `C:\Users\bill\.ao\dev\adaptive-platform-20260918`.
The real Go daemon listened on loopback port 3036; the actual Electron renderer
was inspected through its local CDP port 9336. No mock API, real user data or paid
worker was used. Verified through desktop controls: create Skill, create Codex
Agent Type, attach exact Skill v1, append inactive v2, compare, activate v2,
roll back to v1, inspect audit, clone, disable and retain state on renderer reload.
No renderer errors were observed in the completed interaction pass.

Screenshots inspected: `.cache/adaptive-tests/electron-registry-editor.png`,
`electron-registry-history.png`, `electron-registry-persisted.png`. UI checks
found and fixed the incorrect Board topbar title and unstable accessible names
on populated form fields. These are ignored local evidence, not committed app
state. Full daemon/desktop restart with active workers remains stage 23/25 work.
Native provider/model capability controls, portable bundles, Skill resource
materialization and worker launch are intentionally the next stages.

## Stage 07 — Skill resources and portable definitions (2026-09-18)

- Added Skill resource/tool/MCP requirement editing and inspection, portable
  schema v1 export/import for both registry kinds, CLI commands and four API
  routes. Export embeds exact pinned Skill content while excluding local IDs,
  actors and provider/account references. Missing local bindings remain explicit.
- Imports use one atomic creation transaction for up to 32 Skill dependencies
  plus the root, with disabled state and every manager permission false. An
  invalid later dependency rolls back earlier identities, versions, audit and
  CDC. Unknown JSON fields, schemas, unsafe resource paths and oversized content
  are rejected. File/directory collisions, including the generated SKILL.md
  root, are now rejected before materialization.
- Native Skill materialization and temporary attachments will consume these
  immutable contents through stage 09 worker snapshots; authoring/import does
  not write to provider Skill directories or execute resources.

| Command / check | Result |
| --- | --- |
| Focused registry domain/store/controller/CLI tests | PASS; includes portable round trip, exact historical Skill after newer activation, binding-reference exclusion, strict malformed bundle rejection and atomic rollback |
| Full domain / SQLite / store / API / spec / envelope / skillassets / CLI / telemetry suites | PASS; final domain 1.011s, SQLite 46.409s, store 18.421s, API 0.874s, CLI 20.748s |
| Full HTTP controllers suite | Same two stage 06 Windows failures (mobile config rename and file-URL clone); registry tests pass |
| Pinned golangci-lint v2.12.2 on touched packages | PASS, 0 issues |
| `go build ./...` | PASS |
| Root `npm.cmd run api` | PASS, pinned generator; spec drift tests pass |
| Root `npm.cmd run frontend:typecheck` | PASS |
| Registry/editor/transfer/API client Vitest suites | PASS, 3 files / 44 tests, 6.43s |
| Renderer Vite production build | PASS, 5.22s; existing large-chunk advisory |
| `git diff --cached --check` | PASS |

The isolated Electron and daemon were fully stopped, rebuilt and restarted
against the same scratch profile. The disabled cloned Agent Type from stage 06
was still present. Through actual desktop controls: authored a Skill resource
and tool requirement, created/activated Skill v2, exported that exact version,
downloaded JSON under the scratch AO directory, selected the file for import,
inspected requirements, imported it, and verified resource content plus disabled
state and all three manager permissions off. No renderer errors observed.
Screenshots inspected: `.cache/adaptive-tests/electron-registry-import.png` and
`electron-registry-import-policy.png`. The first download automation attempt was
cancelled by the CDP download behavior; using the page's explicit scratch
download directory verified the actual Download JSON control successfully.
No worker or paid harness call was started.

## Historical initial audit (superseded environment facts)

## Repository and access

| Check | Observed result |
| --- | --- |
| `git status --short` before edits | No changed files; warnings about unreadable user Git ignore file |
| `git remote -v` | `origin` fetch/push is `https://github.com/Untrivial-ai/agent-orchestrator.git` |
| `git branch --show-current` | `main` |
| `git log -1 --oneline` | `684d6d67d fix(session): bound spawn readiness and rollback (#5557)` |
| `git rev-parse HEAD` | `684d6d67db004e180f7ac9c649ce92840491ea4d` |
| `git remote rename origin upstream` | Failed: `could not lock config file .git/config`; remote unchanged |
| `git switch -c feature/adaptive-agent-platform` | Failed: cannot lock ref / create `.git/refs/heads/feature/adaptive-agent-platform` |
| `git ls-remote origin refs/heads/main` | Failed to connect to `github.com:443` |
| `gh auth status` | Reported invalid stored authentication; no credentials printed or changed |
| GitHub connector profile | Authenticated as `2BE-ops`; repository issue/PR reads succeeded |
| GitHub connector fork capability | No fork/create-repository tool exposed in available tool metadata |
| Browser fallback for fork creation | `cua.getBrowser` returned `No browser is available` |

The session permission configuration explicitly grants read-only access to
`.git`, with no approval escalation available. This prevents remotes, branch,
index, commit and worktree setup. It is not just a GitHub authentication issue.
The required architecture commit cannot precede implementation in this session.
No alternate Git directory or relocated clone was used to bypass the restriction.

Fork URL: not created. Requested branch: `feature/adaptive-agent-platform` (not
created). Starting and last incorporated upstream SHA are identical. No fetch,
merge, rebase, feature commit or push has succeeded. GitHub connector PR metadata
does not update local refs or establish the latest upstream SHA.

## Baseline commands executed

These ran before application source modifications (there have been none).

| Command / environment | Result |
| --- | --- |
| `node --version` | `v24.18.0` |
| `npm.cmd --version` | `11.16.0` |
| `Get-Command go` and inspected common install locations | Go not found on PATH; no usable compiler identified |
| `npm.cmd run typecheck` in `frontend` | Failed, exit 1; missing React/JSX, motion, clsx and tailwind-merge resolution in `packages/product-ui`, with downstream type errors |
| `npm.cmd test -- --run src/renderer/components/TaskComposer.test.tsx src/renderer/components/ProjectSettingsForm.test.tsx --maxWorkers=1` in `frontend` | Passed, exit 0: 2 files, 74 tests; Vitest reported 20.06 seconds |
| `npm.cmd ci --ignore-scripts --cache ../../.cache/adaptive-npm --fetch-retries=0 --fetch-timeout=15000` in `packages/product-ui` | Failed, exit 1; registry download returned network `EACCES`; cleanup also reported `EPERM` |

The failed dependency install may leave an incomplete ignored
`packages/product-ui/node_modules` directory. Rerun a real `npm ci` there when
network access is available; do not symlink dependencies from another checkout.
Its diagnostic log is in ignored `.cache/adaptive-npm`; it is not source output
and must not be committed. No package manifest or lockfile was intentionally
changed by the install.

## Not executed / not established

- Backend build, Go tests/race tests/vet/lint, sqlc and API generation.
- Full frontend suite, shared-package checks, desktop package and full e2e gates.
- Clean/existing database migration tests or daemon restart tests.
- Real Electron launch, screenshot inspection, or adaptive UI workflows.
- Paid model calls or authenticated live Claude/Codex/OpenCode worker runs.
- Fork creation, feature commits, branch pushes or remote CI validation.

The passing tests verify existing Task Composer and Project Settings behavior
only. They do not validate any requested new feature. The frontend typecheck
failure is baseline evidence; no claim is made that every downstream diagnostic
will disappear after dependency installation.

## Documentation review

- `git diff --check` passed for the tracked documentation change.
- A local Node check passed for relative links and trailing whitespace in all
  three new documents and confirmed all 48 Definition of Done rows are present.
- Final `git status --short` showed only `docs/README.md` and the three new
  adaptive-platform documents. Application source and manifests are unchanged.
- Final remote/branch/HEAD inspection confirmed the original `origin`, `main`
  and starting SHA remain unchanged. The documents are uncommitted.

## Required environment repair

1. Resume this checkout in a session with write access to its Git metadata.
   Preserve the draft audit/design/checklist. This is required before the
   assignment's design-commit gate and continuous-commit workflow can proceed.
2. Make a Go toolchain compatible with `backend/go.mod` (`go 1.25.7` directive)
   available, and permit dependency installation/regeneration. Use workflow
   runtime versions for final checks; Windows race checks also need a supported
   C toolchain. No system toolchain installation was attempted here.
3. Restore shell network access for Git/npm dependencies and authenticated `gh`
   access for fork/push, or provide an equivalent supported authenticated fork
   capability. Do not paste tokens into the repository or conversation.
4. Enable a supported UI inspection surface for final running-app validation.
   The browser tool currently exposes no browser and native-app APIs are disabled.

GitHub authentication alone does not block local engineering. The immediate
blocking condition is Git metadata access: it prevents the user-required commit
before major implementation. Toolchain and package access separately prevent
verified implementation/build milestones. Independent source audit and design
work was completed while those conditions were recorded.
