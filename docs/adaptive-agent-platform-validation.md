# Adaptive agent platform: environment and validation evidence

Recorded 2026-09-18. The latest milestone evidence below supersedes the historical
initial audit/environment failures retained later in this file. This is not the
final platform validation report.

## Stage 14c1 — durable Manager inbox (2026-09-19)

Migration 0171 retains immutable routing requests with exact task, criteria and
policy references/hashes. Enqueue checks planning authority, current revisions,
frozen criteria, project isolation, cancellation ancestry and current queue limits
in one transaction. One pending request per task also has a SQL guard. Terminal
cancelled/superseded/Needs Human receipts release only inbox capacity. Audit and
trigger-owned CDC commit atomically; no native transport or worker launch occurs.

Focused domain/SQLite/store tests PASS (0.536s/1.143s/0.740s). Tests cover eight
competing distinct submissions against capacity one, eight exact retries with one
creation, bounded history pages, task/policy changes plus reopen, preserved hashes,
worker/Manager and invalid orchestrator authority, cross-project reads/writes,
unfrozen criteria, cancellation, audit-failure rollback with zero partial CDC,
terminal idempotency and raw-SQL immutable/pending guards. Upgrade/down/up preserves
task/policy/CDC; downgrade over inbox history refuses atomically with integrity and
foreign keys intact. Initial test compilation used a nonexistent task edit method;
corrected to the existing ReviseAdaptiveTask boundary before these passes.

Full domain (1.278s), ports, SQLite (39.239s), SQLite helpers, store (23.539s),
Manager service and registry service PASS. An initial full run caught the missing
shipped-migration ledger entry; added 0171 and reran the complete affected suites.
Pinned affected-package lint PASS (0 issues) after comment/error-style fixes.
sqlc and backend build PASS. Complete source/generated diff and whitespace review
PASS. Logs: `.cache/adaptive-tests/stage14c1-*`. API/CLI, native proposals and live
inbox delivery remain subsequent stage-14 work.

## Stage 14b2 — existing native Manager engines (2026-09-19)

Dedicated internal admission now drives the existing TUI/Chat launch and restore
paths. Exact retries return retained sessions before live registry/readiness work;
native generation reservations resolve only after connection. Uncertain launch
retains its dispatch and pending operation. The distinct Manager role uses sealed
Type instructions and native Skill files, excluding worker/orchestrator rules.
Production start, durable inbox and structured tools remain subsequent work.

Focused native Manager tests PASS (0.641s); configured-worker/task execution
regressions PASS (0.693s). Full-suite investigation found one new redundant-read
regression in ordinary restoration; removed that read and the combined focused
regression suite PASS (0.913s). Reopened SQLite tests verify exact Manager TUI and
Chat configuration/resources, replay without readiness effects, forged admission
rejection before side effects and failed native launch without duplicate launch.

Full session-manager suite FAIL (42.022s) only at its eight recorded Windows
baselines, matching stage09c5 (handoff/switch paths, permissions, transcript lookup,
dev namespace and executable PATH). Ports, chat and session service suites PASS;
the final rerun reused passing service results (prior chat 38.400s/session 53.310s).
Backend build and pinned affected-package lint PASS (0 issues). An initial lint
negation suggestion was corrected. Source diff and whitespace review PASS. Logs:
`.cache/adaptive-tests/stage14b2-*`. No live provider test is claimed.

## Stage 14b1 — durable Manager admission and native fences (2026-09-19)

Migration 0170 retains one unreleased controller per project, atomic native session
and frozen configuration binding, execution intents/resolutions and immutable
audit. Generic configured workers retain their existing role restriction. Native
effects are not wired yet; these store methods define the required lifecycle gate.
Current policy and exact Type/Skills are checked at seed time. Replayed requests
inspect existing records; a second dispatch cannot follow a resolved first launch.

Tests cover eight competing admissions and eight competing seeds, exact replay,
changed idempotency data, floating/overridden configuration, disable between reserve
and seed, unseeded cancellation, stale desired policy, separate project lookup,
injected seed/release audit failure with zero partial state or CDC, disabled-Type
history and reopen. Native tests cover the reserved target generation, unresolved
effects across reopen, failed/stale resolution, exact resolution retry, competing
restore/release, SQL history/release guards and released-session resurrection.
Migration tests retain governance/CDC through empty down/up and atomically refuse
downgrade with retained ownership, then check integrity and foreign keys.

Focused domain/SQLite/store PASS (0.447s/1.143s/0.883s). Full domain (1.040s), ports
(0.480s), SQLite (42.615s), SQLite test helpers (2.360s), store (22.928s), Manager
service (0.279s) and registry service (1.785s) PASS. sqlc, backend build and pinned
affected-package lint PASS (0 issues). One initial test command used the wrong
working directory and did not execute; corrected focused/full commands above ran.
Complete source/generated diff and whitespace review PASS. Logs:
`.cache/adaptive-tests/stage14b1-*`. No live controller/process claim is made.

## Stage 14a3 — Manager governance API and CLI (2026-09-19)

The daemon wires a shared Manager service, project-scoped governance PUT/GET and
immutable configuration/audit reads. Strict 64 KiB decoding rejects actor/origin
fields, trailing objects and malformed policy; the service denies nonhuman
governance before persistence. Five `ao agent-manager` commands remain HTTP-only,
with bounded pages, exact version reads and preserved daemon error/request IDs.
OpenAPI/TypeScript generation, telemetry route templates and the embedded CLI
catalog accompany the API. There is still no native launch effect in configuration.

Focused service/HTTP PASS (0.417s/0.520s), focused CLI PASS (0.386s). Tests cover
CAS conflict envelopes, exact history, cross-project isolation, unknown/duplicate
page parameters, body limits, forged authority, invalid versions/policy, missing
service, stdin transport and usage exit codes. Full service (0.526s), HTTP router
(0.858s), spec (0.366s), specgen (16.481s), envelope (0.788s), CLI (27.654s),
telemetry (0.588s) and embedded skill assets (0.492s) PASS. Full HTTP controllers
FAIL (17.922s) only at the recorded Windows pairing rename and file-URL clone
baselines. Full daemon FAIL (3.403s) only at the recorded CWD TempDir cleanup test.

API regeneration, backend build, frontend typecheck and affected-package pinned
lint (including daemon, 0 issues) PASS. All 40 frontend API-client tests PASS
(2.53s), including Manager identity redaction. A first gofmt invocation used root
paths from backend and failed; the corrected root invocation completed before
the full checks. Diff review removed map alignment churn; generated schema/route
changes and new source were inspected. Logs: `.cache/adaptive-tests/stage14a3-*`.

## Stage 14a2 — versioned Manager governance (2026-09-19)

Migration 0169 adds project Manager configuration history, user provenance and
transactional audit/CDC. Exact controller Type versions and bounded policies are
sealed; disabled Types do not destroy history or prevent disabling governance.
Only human-authored CAS changes are accepted. Configuration has no native launch
effects. Quotas are retained policy, not a claim of implemented selection/actions.

Tests exercise default creation denial, invalid bounds, forged Manager/worker/
orchestrator/system authority, borrowed session identity, eight competing writers,
stale edits, missing references, reopen, immutable rows and injected audit failure
with complete policy/CDC rollback. Migration tests preserve prior CDC, permit empty
down/up, reject unknown events and refuse downgrade over retained policy history
atomically, with integrity/foreign-key checks. Initial focused tests exposed the
missing CDC CHECK vocabulary; the additive guarded migration fixes it.

Full domain (0.772s), ports (0.337s), SQLite (37.593s), store (17.187s), SQLite test
helpers (1.172s) and CDC (1.103s) PASS. Backend build and pinned affected-package lint
PASS (0 issues). sqlc regenerated from queries/migration. Complete diff/whitespace
review precedes commit. Logs: ignored `.cache/adaptive-tests/stage14a2-*`.

## Stage 14a1 — distinct Agent Manager identity (2026-09-19)

Migration 0168 transactionally widens the session-kind CHECK using the existing
schema-edit approach, preserving parent-table foreign keys/CDC. The new
`agent_manager` role requires a project, retains immutable role/project identity
and permits at most one nonterminated manager per project. Manager Chat uses the
existing session-scoped conversation model, separate from the orchestrator's
project narrative. Generic API/service spawn denies manager admission. Dedicated
durable policy/inbox/native admission follows; this is not a live manager claim.

Tests cover upgrade/downgrade, retained legacy facts/CDC, schema integrity,
atomic downgrade refusal with terminated manager history, eight racing seeds,
separate project controllers, conversation isolation, reopen, retained old
identity and rejection of resurrection over a replacement. Focused SQLite/store/
session/HTTP PASS (2.061s/0.539s/0.097s/0.118s). Full domain (1.275s), ports
(0.489s), SQLite (47.339s), store (29.570s), session service (60.147s), HTTP router
(0.764s), API spec (0.336s), specgen (18.193s) and envelope (0.741s) PASS.

Full HTTP controllers FAIL (20.700s) only at the recorded Windows pairing rename
and file-URL clone tests. Full affected lint reports two unchanged G115 findings
at `service/session/path_resolve_windows.go:38,42` (int to uint32 buffer length),
outside this slice. They remain final platform-validation work, not passed lint.
sqlc regeneration PASS with no generated changes. Logs: ignored
`.cache/adaptive-tests/stage14a1-*`.
Backend build and changed-code pinned lint (`--new-from-rev=b410f1200`, 0 issues)
PASS. Complete staged diff and whitespace checks PASS.

Periodic upstream fetch succeeded. Observed upstream main is now
`1e4a394b20c470d281b2a7f6f63fd47c30af5019`; migrations still end at 0147, so
0168 does not collide. Six commits since the prior observed `795286c4e` cover
workspace diff inspection, mobile UX, landing asset size, chat/board styling and
editor handoff. None changes session-kind/adaptive storage. These are not yet
incorporated; final integration remains stage 26. Incorporated base stays
`6d3ad8c7c`.

## Stage 13d2b — grouped metrics and performance API/CLI (2026-09-19)

One consistent transaction summarizes up to 1000 admitted attempts by Type,
Type version, Skill, Skill version, harness, model, category or capability. Total
counts retain unseeded/mixed work; configuration groups expose exclusions. Skill
and capability overlap is explicit. Historical assessed passes, first-pass rules,
result revisions, retry/CI/review counts, closed duration samples and known-usage
denominators remain distinct. Oversized cohorts, duplicate identities and numeric
overflow cannot yield partial metrics. No score or causal comparison is stored.

New daemon evidence/summary routes, `ao task performance`/`ao task metrics`, typed
OpenAPI/frontend contracts and telemetry route templates use the shared service.
Windows/cursors/groups are validated; unsupported or repeated API parameters fail
instead of being silently ignored. HTTP/CLI error envelopes and request IDs are
retained. Raw evidence shows null counters separately from zero; totals show
known sample and priced-event counts. CLI documentation explains all denominators.

Focused domain/store/task/CLI PASS (0.691s/0.925s/0.550s/0.356s). Full domain
(1.477s), ports (0.414s), SQLite (48.744s), store (31.634s), task (2.370s), CLI
(29.836s), telemetry metadata (0.660s), HTTP router (0.799s), API spec (0.370s),
specgen (15.246s) and envelope (0.621s) PASS. Full HTTP controllers FAIL (21.849s)
only at known Windows `TestBridgeStatusConcurrentSecurePairing` and
`TestProjectsAPI_Clone`; new performance HTTP integration passes with real SQLite.
API generation, backend build, frontend typecheck, 39 frontend API-client tests
(1.61s), pinned affected lint (0 issues) and diff/whitespace inspection PASS.
Final full CLI after aligning zero-time validation PASS (17.045s).

A frontend test initially passed a query string to the pathname-only telemetry
normalizer; its caller already strips queries with `URL.pathname`. The test now
matches that contract; no unrelated telemetry behavior was changed. Initial lint
requested command slice capacity; fixed before the final lint pass. Logs are
ignored `.cache/adaptive-tests/stage13d2b-*`. This completes stage 13 foundations;
desktop performance, live providers, scheduler and recovery integration remain
their later stages. No new migration or desktop authoring flow is added here.

## Stage 13d2a — attempt performance evidence (2026-09-19)

The SQLite projection reads bounded admission cohorts in one transaction, retaining
unseeded attempts, exact historical configuration/result/assessment identities,
mixed configuration flags, witnessed review changes, historical CI failures and
first-pass evidence. Usage comes from existing native accounting; unknown tokens
stay null, priced event counts accompany partial cost, and overflow returns an
error without partial rows. Duration measures reservation time and distinguishes
ongoing ownership. This adds queries/domain/store only, without a migration.

Focused performance/configuration tests PASS (0.710s). Full ports (0.394s), SQLite
(27.490s), store (13.919s) and sqlitetest (1.110s) PASS. A new domain boundary test
caught control characters in cursors; after fixing validation, full domain/task
PASS (0.552s/1.761s). Final full store/task after lint fixes PASS (13.029s/0.897s).
Tests also cover cohort boundaries/cursors/project isolation, retained identity
after disable/reopen, first-pass rules, result corrections, evaluation retries,
mixed interface changes, native review launch witnesses, zero/missing/estimated
usage and integer overflow. sqlc generation, backend build and pinned affected
lint PASS (0 issues). Complete diff/whitespace inspection PASS.

The first combined command used the nonexistent `service/tasksvc` directory;
the corrected task suite passed as above. That failed invocation is not labeled
passed. Existing Windows full-suite/race gaps remain recorded below. Ignored
logs: `.cache/adaptive-tests/stage13d2a-*`. Aggregates/API/CLI follow in 13d2b;
performance desktop views remain stage 22 and live validation stage 25.

## Stage 13d1 — derived current task completion (2026-09-19)

Task reads now expose current completion evidence IDs/reason and a completed
phase. One SQLite transaction checks latest attempt/result/assessment, current
revision, cancellation and fresh stored SCM/review facts. A historical passing
assessment cannot hide a new result, review pass, failed check or changed PR head.
Verified immutable Git blob evidence is reused for its exact commit; no filesystem
or native process I/O runs inside SQLite. Completion never releases ownership or
rewrites historical assessments. Scheduler dependency/admission integration of
the shared predicate remains stage 16.

Three SQLite completion tests cover absent assessment, current success, retained
lease, changed/failed checks, cancellation/resume, result correction, criteria
revision, reopen, immutable artifacts, changed heads, native exit and a new
attempt. Existing native review integration now verifies completed API output
with an active lease and loss of current completion when a newer review starts.
Focused completion/review tests PASS (0.711s).

Full domain (1.403s), ports (0.445s), SQLite (32.490s), store (16.313s), task service
(1.625s), CLI (19.984s), API spec (0.192s) and specgen (7.181s) PASS. After tightening
the current PR observation guard, full store/task rerun PASS (14.619s/1.172s).
sqlc/API generation, backend build, frontend typecheck and affected pinned lint
PASS (0 issues). Complete diff/whitespace inspection PASS. Full HTTP controllers
remain FAIL (9.150s) only at recorded Windows baselines
`TestBridgeStatusConcurrentSecurePairing` and `TestProjectsAPI_Clone`; new API
completion checks pass. Logs: ignored `.cache/adaptive-tests/stage13d1-*`.

Attributable performance/usage history remains stage 13's next increment. This
slice adds no migration and no desktop authoring flow.

## Stage 13c3f — independent review attribution in evaluations (2026-09-19)

The collector separates generic history from native passes for the exact result,
retains the latest PR/harness pass including running/failed states, and verifies
sealed review context in the evaluation transaction. Compact attribution records
exact result/criteria/configuration hashes, implementing/reviewing Type versions,
model, and native launch witness; full Skills remain in the inspectable context.
Review criteria require matching policy/provenance and independently observed
current PR heads. Reasons explicitly label reviewer verdicts qualitative.

Seventeen domain cases cover approval, generic/unwitnessed/future/stale/mismatched
evidence, policy violations, changes requested and newer incomplete passes.
Real SQLite tests exercise generic-only approval, fast unwitnessed reply, launch
witness, passing native review, newer running/failed/changes-requested passes,
same-commit result correction, Type disable and historical retry after reopen.
An approved review cannot satisfy a missing objective CI criterion. Optional
attribution fields preserve prior evaluation JSON/hash compatibility.

Focused new domain/store tests PASS (0.490s/0.497s). Full domain (1.086s), SQLite
(36.626s), store (20.450s), task service (1.578s), review service (0.148s), API spec
(0.245s) and specgen (12.039s) PASS. Focused HTTP evaluation/review tests PASS
(0.762s). sqlc/API generation, backend build and pinned domain/SQLite lint PASS
(0 issues). Frontend typecheck PASS. Complete
diff inspection and whitespace check PASS. Logs: ignored `stage13c3f-*`.

This completes stage 13's pinned native-review evidence path. Derived task
completion and attributable performance/usage remain stage 13 work; native
restart reconciliation and live provider runs remain stages 23/25.

## Stage 13c3e — native review request and inspection API/CLI (2026-09-19)

The task service resolves an exact result's frozen reviewer policy through the
shared registry, seals native configuration and invokes the existing reviewer
service/engine behind Codex account admission. Three HTTP routes and three thin
CLI commands request review, list up to 64 passes for a result, and inspect a
single retained configuration/criteria/launch witness. Strict 8 KiB requests
exclude actor, configuration and verdict input. Store insertion additionally
fences explicit Type settings and actor-to-selection provenance.

Real SQLite + task/review services + engine + HTTP tests use an injected native
launcher. They verify retained-before-launch ordering, policy pins, scoped reads,
invalid/worker denial, running/approved retries, result submission, history after
Type disable, missing binary versus uncertain launch, and Codex account gating.
They do not claim a live provider run. CLI checks cover all three routes, usage
errors and preservation of the daemon error code/request ID.

Focused task-review/API/CLI tests PASS (store 1.026s, controllers 0.496s, CLI
0.262s). Full domain/ports, SQLite (37.686s), store (18.302s), task service
(1.649s), review service (0.149s), CLI (21.527s), telemetry (0.562s), HTTP router
(0.594s), API spec (0.251s), specgen (8.981s) and envelope (0.634s) PASS. Final
CLI rerun after sharing request transport PASS (16.767s). Focused daemon wiring
PASS (1.142s). sqlc/API generation, backend build, frontend typecheck and pinned
affected-package lint PASS (0 issues). Full diff/whitespace inspection PASS.

Full controllers remain FAIL (11.293s) only at the two recorded Windows baseline
tests: `TestBridgeStatusConcurrentSecurePairing` (mobile.json rename denied) and
`TestProjectsAPI_Clone` (Windows file URL validation). They are not relabeled as
passed. Logs: ignored `.cache/adaptive-tests/stage13c3e-*`. No migration or desktop
authoring surface changed. Live reviewer flows remain stage 25; evaluator review
attribution and derived completion/performance remain stage 13's next work.

## Stage 13c3d — generation-fenced native review results (2026-09-19)

Task review submissions require the retained launch generation and matching
worker. The running-to-complete transition checks current reviewer ownership and
commits the result with its task audit in one transaction. Exact historical retries
acknowledge the same verdict/body/provider review ID after delivery, replacement
or restart; changed retry content is rejected. The generic review update can only
fail/cancel a task review with an empty verdict. A fast reply may be recorded
before native handle persistence, but cannot invent the separate launch witness.

Single and batch HTTP/CLI submissions preserve sourceGeneration. Cancelled task
results cannot strand valid sibling reviews. Telemetry emits only on the actual
persisted result transition and contains the existing enum/count fields, never
review prose or context identities. Existing generic review behavior is preserved.

Focused store/service/CLI/HTTP submission tests PASS (0.834/0.091/0.863/0.099s).
Full domain, ports, SQLite (41.035s), store (26.143s), review service (0.134s),
task service (1.735s), CLI (26.438s), API spec (0.230s) and specgen (14.525s)
PASS. sqlc and API/TypeScript generation, backend build and pinned affected-package
lint PASS (0 issues). Final store rerun PASS (13.546s), frontend typecheck PASS,
and complete staged diff inspection/whitespace check PASS. No migration or desktop authoring flow
changed. Ignored logs: `.cache/adaptive-tests/stage13c3d-*`.

Review request/inspection through the shared task service and evaluator review
attribution remain the next stage 13 increment; this is not a completion claim.

## Stage 13c3c — native task review engine and payload delivery (2026-09-19)

The existing review engine accepts sealed task context, filters exact PR heads
and effective review scope, persists before launch, and records the native launch
witness only after the matching handle/launch ID is saved. Repeated active or
approved scopes do not spawn again; another running scope cannot be preempted.
Runtime-creation/initial-delivery uncertainty keeps the running reservation,
including across a new engine instance. Known preflight/spawn refusal retains a
failed pass without a launch witness. No reviewer configuration is inferred from
an implementing worker or a generic approval.

Native launch materializes pinned Type/Skill instructions and resources through
the shared worker resource boundary. Context/task/system files are immutable and
isolated per pass under AO data. Skills lie inside that pass's allowed prompt
directory (including OpenCode's external-file permission boundary). The native
conversation identity includes the launch ID; previous Type history cannot be
inherited or notified in place. Claude, Codex and OpenCode explicitly advertise
prompt-context consumption. Unsupported adapters, Chat settings, provider
bindings/native options and incompatible permissions fail before runtime effects;
these are reported compatibility limits, not silently dropped configuration.

Six new engine/invocation tests cover durable-before-spawn ordering, exact
configuration and files, immutable-byte checks, denied unsupported inputs,
scope retries, non-preemption, known failures and uncertain launch/restart.
Focused trigger/restore/task-review tests PASS (0.933s); worker configuration,
resource and context tests PASS (1.380s). Final review-service PASS (0.085s),
OpenCode and ports PASS; backend build and pinned review/reviewer/ports/manager
lint PASS (0 issues). Complete staged diff inspected; whitespace check PASS.
No wire DTO changed, so API/type generation was not needed in this slice.

Full review engine retains its Node-shim PATH failure (1.889s final run); full
session manager retains the eight recorded Windows failures (39.452s). The full
reviewer-adapter sweep additionally exposes existing command-path test failures:
agy/devin/droid/kimi use POSIX absolute-binary fixtures on Windows; Claude restore
cannot resolve its test command; Codex effort's test binary is absent from PATH;
Cursor expects POSIX modes and a different source-auth path. Their command and
restore implementations are unchanged by this slice. These suites are **FAIL**,
not claimed passed; resolve/validate them in stage 24. Logs: ignored
`*stage13c3c*`. Shared service/API/CLI and generation-checked submissions are next.
Pinned native reviewer restore currently refuses generic configuration substitution;
retained native-context reconciliation remains stage 23, not a completed recovery claim.

## Stage 13c3b — retained native review provenance (2026-09-19)

Migration 0167 adds an effective scope to existing review runs and an immutable
context record; no duplicate review lifecycle. The store seals frozen criteria,
result/commit, implementing configuration and reviewer configuration with the
run and task audit. It rejects caller-forged provenance, worker authority,
changed results, changed PR head/ownership, unavailable registry references and
overridden Type instructions. The generic insert path cannot assert task scope.
Review context is bounded to 2 MiB and 64 passes per result (failed passes count).
The launch timestamp is recorded once only for a matching launch ID and handle.
It is a launch observation, not an acceptance verdict.

Three real-SQLite context tests PASS (0.664s): forged references, exact retry
scope, result replacement, audit failure rollback, native launch fencing,
history/caps, generic scope coexistence, registry disable/reopen and SQL
immutability violations. Upgrade/downgrade test PASS (0.881s). Full domain,
ports, task and review-service suites PASS. Final SQLite suite PASS (34.141s),
store PASS (19.932s), spec PASS (0.268s), specgen PASS (9.571s).
Full review engine FAIL retains the previously recorded
`TestLauncherSpawnPrependsNodeRuntimeForNodeShimReviewer` Windows PATH baseline;
no launcher implementation changed in this slice. `npm run sqlc`, `npm run api`,
`npm run frontend:typecheck`, backend build and pinned domain/ports/SQLite lint
PASS (0 issues). Full staged diff inspected, whitespace check PASS. Logs:
ignored `*stage13c3b*`. Native consumption, generation-fenced submission and
evaluation attribution are next; persistence alone is not launch validation.

## Stage 13c3a — frozen reviewer policy (2026-09-19)

Acceptance criteria optionally pin an exact reviewer Agent Type/version and
independent-Type/harness requirements. Planning validates enabled Type identity
and historical version within its existing transaction. Failed pins leave no
partial task/audit or revision; workers cannot remove review requirements.
Legacy criteria omit the new field and retain their original hash. A different
version of the same Type does not satisfy the different-Type requirement.

Domain violation tests, a real-SQLite reservation/revision/registry-edit/reopen
test, and HTTP create/inspect/reject tests PASS. Full domain (1.051s), SQLite
(40.152s), store (25.321s), task (1.763s), taskcontext (0.474s), API spec (0.281s)
and specgen (15.852s) suites PASS. Focused AdaptiveTask HTTP tests PASS (1.480s).
`npm run api`, `npm run frontend:typecheck`, backend `go build ./...` and pinned
domain/SQLite/task/controllers/apispec lint PASS (0 issues). Complete staged
diff inspected; `git diff --cached --check` PASS. Logs: ignored
`*stage13c3a*`. This is policy authoring/persistence, not yet proof of native
reviewer consumption; that integration is the next slice. No migration needed.

## Stage 13c2b — build/test/lint evidence through existing CI (2026-09-19)

The test/build/lint criterion kinds now accept frozen exact check names and reuse
the existing independent CI collector. The same full-commit/current-head/snapshot
membership, success, missing/pending/skipped and truncation rules apply. Command
vectors remain inert expectations; ambiguous command-plus-check selectors are
rejected. Existing command-only criteria and their hashes remain unchanged and do
not pass from worker claims. Following mission §§24/28 and the reuse decision,
the earlier proposed separate command executor is replaced by this existing
evidence boundary; CI results are never labeled as local daemon command execution.

Domain tests cover all three kinds with success/failure/pending/skipped states,
legacy expectations and ambiguous input. Real-SQLite test proves worker claims
remain inconclusive until the named independent checks arrive, then retains exact
configuration and separate criterion attribution. Focused domain/store PASS
(0.480s/0.454s). Full domain, SQLite (37.804s), store (22.208s), task/context,
artifact collector and HTTP spec suites PASS. Pinned domain/SQLite lint PASS
(0 issues) and backend build PASS. No generated contract or UI shape changed.
Logs: ignored `*stage13c2b*`. Native reviewer Type/criteria provenance, completion
rules and usage/performance denominators are next.

## Stage 13c2a — independent commit-bound artifacts (2026-09-19)

Artifact criteria may freeze a portable path and expected SHA-256. Production task
evaluation authorizes preparation before invoking the local Git collector outside
the SQLite transaction, then repeats result/version/actor fences on final write.
Exact retries return historical evidence before collector access. Collected facts
are trusted daemon context, excluded from request JSON and validated against the
frozen criterion/path/commit. No migration is needed; optional fields preserve old
criteria and evaluation hashes. Legacy artifacts without an expected hash stay
inconclusive rather than passing arbitrary prose requirements.

The adapter reads raw regular Git blobs, not dirty files or archive/checkout
filters. Replacement objects, inherited Git variables, lazy fetching and fsmonitor
are disabled. Nothing is fetched, executed or written into the repository. Bounds:
16 artifacts, 1 MiB each and 15 seconds overall. Missing paths/hash mismatches fail;
missing commits, unavailable Git, symlinks, submodules and oversize remain unknown.
Output limits include io.Copy paths; diff review caught and removed a promoted
bytes.Buffer.ReadFrom bypass before final verification.

Two real-Git adapter tests, domain verdict matrix and real-SQLite service/collector
integration PASS. They cover dirty/export-ignored files, literal pathspecs, missing
commits/paths, symlink objects, oversize/cancellation/count bounds, frozen hashes,
actor denial before Git access, retry without source access, collector failure and
result replacement during collection. HTTP task fixtures use the production
collector option; focused results/evaluations tests PASS (store 1.824s, HTTP 0.680s).
Full adapter/domain/SQLite (36.809s)/store/task/context/ports suites PASS. Final full
store 25.329s, HTTP router/spec/envelope and CLI 24.641s PASS. Full controllers
retain the two recorded Windows failures; full daemon retains its recorded cwd
cleanup failure. Final adapter regression after the output fix PASS (1.137s).
API generation, frontend typecheck, backend build and pinned affected-package lint
PASS (0 issues); final API drift/CLI focused checks PASS. Logs: ignored
`*stage13c2a*`. Native reviewer provenance, bounded command verification, usage/
performance and derived completion remain stage 13 work.

## Stage 13c1 — PR, review and lifecycle observations (2026-09-19)

Evaluations now snapshot up to 16 independently observed PR records, 32 latest
review runs per PR/harness at the exact result commit, and session/lease facts in
the evaluation transaction. SQL bounds loaded review text before a 4 KiB UTF-8
preview is retained with its hash, original byte count and truncation flag.
Review prose remains qualitative: generic approval cannot satisfy task criteria,
and harness does not imply an Agent Type. Reservation time is ongoing until lease
release; termination is not labeled a crash. Optional new snapshot fields preserve
old hashes. No schema migration or native process change is needed for this slice.

Frozen `mergeability` criteria pass only with observed, non-draft, mergeable PRs at
the exact result commit. Conflicts/closed-unmerged facts fail; stale, unknown or
truncated collections do not pass. Two domain tests and three real-SQLite store
tests cover the verdict matrix, old hashes, superseding review runs, Unicode and
collection bounds, release facts, immutable history/retry and reopened SQLite.
The HTTP assessment test verifies lifecycle evidence reaches the API.

Focused store/controller PASS (1.204s/0.500s). Full domain, SQLite (32.637s),
store, review service, task service and SCM suites PASS; final full store
(18.328s), HTTP router/spec suites PASS. sqlc/API generation, backend build,
frontend typecheck and pinned affected-package lint PASS (0 issues). Full review
engine suite has one Windows failure in the unchanged launcher test
`TestLauncherSpawnPrependsNodeRuntimeForNodeShimReviewer`: actual PATH lacks the
expected Node shim directory. Keep this exact gap for stage 24; it is not a local
pass. Initial fixture field/method compile errors were corrected before final
checks. Logs: ignored `*stage13c1*`. Independent reviewer Type/criteria provenance,
local verification, usage/performance and derived completion remain stage 13 work.

## Stage 13b — evaluation service, API and CLI (2026-09-19)

Three scoped task/attempt routes expose collection, paged history and exact
assessment inspection through the shared task service. The strict 8 KiB request
contains only a result ID, expected version, retry key and reason. Callers cannot
supply evidence, verdicts, criteria or actor identity. HTTP-only `ao task evaluate`,
`evaluations` and `evaluation` commands preserve daemon error codes/request IDs.
Generated OpenAPI/TypeScript contracts, command docs and telemetry path redaction
are updated. Shared pagination also retains the existing result commands.

Two real-SQLite HTTP tests, service failure/authority test, CLI usage/error test
and three CLI route cases PASS. They cover exact retries, stale versions/results,
cross-task/attempt rejection, history, audit, malformed/forged/oversized bodies,
no ownership/planning mutation, and no conversion of worker claims into success.
Focused task/CLI/controller run PASS (0.681s/0.815s/0.529s). Full task, CLI,
HTTP router/spec/envelope, telemetry and skillassets suites PASS; the full
controller suite retains exactly the recorded Windows mobile-pairing rename and
file-URL clone failures. Final full CLI rerun PASS (20.374s), pinned affected-package
lint PASS (0 issues), backend build PASS, API generation PASS, frontend typecheck
PASS, and all 39 API-client tests PASS. Initial test field-name and linter
preallocation findings were corrected before final validation. Logs: ignored
`*stage13b*`. Independent command/artifact/review evidence, derived completion and
performance denominators remain stage 13 work; desktop inspection remains 21/22/25.

## Stage 13a — immutable CI evaluation evidence (2026-09-19)

Migration 0165 retains at most 64 immutable assessments per attempt, with exact
result/criteria/context and configuration-at-submission attribution. Collection
accepts no caller verdict or evidence; it reads stored SCM facts and commits the
assessment with audit/trigger CDC atomically. Expected versions and stable keys
fence competing collections and preserve historical retry acknowledgements.
Only the latest result may receive a new assessment. Evaluations never release
leases, modify planning or execute worker-reported commands.

Frozen criteria now support `ci` with 1–16 exact check names. Older definitions
retain identical JSON/hashes. Passing requires every named check's success on the
exact result commit and current PR head, with matching per-check/complete-snapshot
observation provenance. Migration 0166 adds nullable per-check observation time;
old rows remain unknown until actually observed. This prevents retained checks
absent from a newer snapshot from satisfying criteria. Times describe the stored
observation, not every provider refresh; no arbitrary age cutoff is claimed.
Missing, pending, skipped, cancelled, stale-head or truncated evidence cannot pass.
The bounded collector retains 128 relevant checks and their source URLs. Other
criterion kinds remain inconclusive until their independent collectors are added.

Two domain tests, six store tests and the new upgrade/downgrade test PASS, plus the
populated task migration regression. Coverage includes legacy hashes, frozen
criteria, no trust in worker claims, removed checks, complete evidence, exact
configuration across TUI/Chat activation, concurrent collection, actor restrictions,
audit rollback, immutable history, restart, pagination and collection/history caps.
Focused domain 0.482s, SQLite 1.436s, store 0.874s. Final full domain/SQLite/store,
SCM observer and HTTP/router/spec suites PASS (SQLite 34.366s, store 17.183s).
Full task/context/session (60.329s), ports/CDC, CLI (19.436s), GitHub/GitLab adapters,
frontend typecheck, backend build, sqlc/API generation and pinned affected-package
lint PASS (0 issues). Logs: ignored `*stage13a*`.

Initial checks found the missing migration-ledger additions and an import grouping;
both were fixed before the final full rerun. One check command named a nonexistent
router subpackage; the corrected full root HTTP suite passed. Service/API/CLI
evaluation access, remaining evidence collectors, independent reviewer provenance,
derived completion and performance aggregation are subsequent stage 13 slices.

## Stage 12e — native worker output instructions (2026-09-19)

Reserved TUI and Chat launches now seal HTTP-only result/message argv, JSON request
examples, the owning daemon run-file environment, task/session/attempt identity
and the reserved native generation into their bounded input. Instructions explain
partial claims, independent evaluation, idempotency/corrections, message threading,
field limits and uncertain delivery. Literal JSON argv/environment avoids shell
interpolation. Unresolvable/invalid executables or combined prompt overflow reject
before native execution while retaining the task reservation. The embedded
using-ao catalog documents task output, inspection and knowledge commands.

Two native integration tests cover both modes: actual generated examples pass
the shared result/message services with real SQLite; exact retries are idempotent,
claims retain leases, stale generations fail, and malformed/oversized instructions
never launch. Task/context/native focused regression PASS (1.113s). Full task,
context, message, Chat (34.364s), store (15.660s), ports/domain, CLI (17.190s), and
skillassets suites PASS. Full manager retains exactly the eight recorded Windows
baseline failures (42.224s; final run-file refinement rerun 37.327s), with all new
tests passing. Backend build and final staged changed-code pinned lint PASS
(0 issues). Logs: ignored `*stage12e*`. No API shape or migration changed.

Stage 12's initial-launch protocol is tested. Controller replacement deliberately
cannot refresh authority from a historical prompt. Stage 23 must deliver and
journal replacement-generation instructions through the native ownership boundary,
including live Chat host adoption; it must preserve sealed original context and
fence stale processes. Stage 20 resolves uncertain message recipients; stage 25
validates actual provider submission/consumption. These remain explicit gaps.

## Stage 12d — prior findings and incoming contracts in context (2026-09-19)

The bounded context builder selects the latest correction from up to three prior
attempts and the latest worker result matching each frozen dependency revision.
A projection carries findings, unresolved issues and interface claims, excluding
executable test commands. The manifest pins both the original result hash and the
exact projected content hash. Up to eight recent incoming interface proposals
retain their full immutable attribution. Candidate limits and prompt budgets
record omissions; context inclusion does not acknowledge outbox delivery.

Three real SQLite tests cover correction selection, dependency revision changes
before/after reservation, explicit candidate omissions, unrelated/wrong-target/
forged artifact rejection, and historical context after new messages and restart.
Focused context tests PASS (1.005s). Full domain, SQLite (34.678s), store (18.023s),
task/context/message suites PASS. Backend build, sqlc and full affected-package
pinned lint PASS (0 issues). Existing native context regression also PASS. Logs:
ignored `*stage12d*`. Native submission instructions are next; live consumption
remains stage 25. No API shapes or migrations changed in this slice.

## Stage 12c2b — native message delivery and restart recovery (2026-09-19)

The daemon starts the outbox consumer after native/session startup reconciliation
and drains it before controller shutdown. Migration 0164 persists a fair scan
cursor, with at most 16 candidates per five-second cycle. Readiness probes defer
busy, blocked, expired or unknown recipients without consuming send attempts.
Each native call is bounded to ten seconds; a separate three-second commit budget
retains its outcome on cancellation. Restart converts unresolved prior claims to
uncertain without replay. A failed branch read does not stop unrelated messages.

TUI delivery reuses guarded coordination under the existing exclusive operation
fence and exact-generation pre-write check; only idle/input-ready workers receive
it. Chat uses the existing keyed automation relay and its controller queue. The
escaped payload is visibly attributed worker information, not planning authority.
Runtime errors after a write starts remain uncertain. Review also fenced subsequent
messages to a recipient with an unresolved/uncertain write, avoiding concatenation
with a possible partial paste. This fence's human resolution belongs to stage 20.

Four dispatcher tests and three native transport tests PASS (focused message
service 0.283s, manager 0.822s, store 1.247s). They cover durable-before-send ordering,
fair cursor restart, lost commit acknowledgement, no duplicate send, independent
branch progress, shutdown outcomes, TUI/Chat routing, busy/blocked/stale/unknown
refusal, exit at the final write boundary, escaped attribution and partial-write
uncertainty. SQLite tests additionally reopen the cursor and fence later sends to
an uncertain recipient. The populated migration test upgrades/downgrades 0164.

Full domain/SQLite/store/CDC/task/context/message/Chat/session suites PASS (SQLite
46.854s, store 24.175s, Chat 43.141s, session 55.641s). Final affected persistence
rerun PASS (SQLite 29.704s, store 12.473s). Full sessionguard/supervisor PASS; full
manager retains the same eight Windows baseline failures, daemon the recorded cwd
TempDir cleanup failure. Backend build, sqlc and changed-native plus full new
package/domain/ports/SQLite pinned lint PASS (0 issues). Logs: ignored `*stage12c2b*`.
An unused test import was removed before focused validation. Native adapters here
are recording fakes; authenticated provider and Electron consumption remain 25.

## Stage 12c2a — typed message API, timeline and CLI (2026-09-19)

Three daemon routes provide strict 64 KiB submission and project-scoped timeline
and detail reads, shared by worker/controller tools and UI. Optional task filtering
includes both sent and received messages. Stable sequence cursors page immutable
content; detail separates retained messages from delivery observations. Public
delivery provenance exposes the target native generation while the internal
controller owner stays off the wire. Result and message services share derivation
of worker identity; storage independently rechecks ownership on commit.

Two real SQLite HTTP tests exercise persistence before any send, duplicate retries,
conflicting keys, task/project scope, paging, missing references, strict JSON and
stale/forged authority. A service test covers TUI/Chat attribution and failure
envelopes; two CLI tests cover usage/budgets/errors and exact filter encoding, with
three additional HTTP-only command boundary cases. Focused tests PASS (CLI 1.178s,
task 0.614s, controllers 0.646s, store 1.789s). Full CLI (27.430s), task, telemetry,
domain, store (22.948s), router/spec/envelope PASS. Full controllers, including the
final decoder rerun, retain only the two recorded Windows pairing/clone failures.
Frontend client tests PASS (39); frontend typecheck, backend build, API generation
and affected-package pinned lint PASS (0 issues). Semantic API comparison confirms
zero changes to existing paths/schemas. Logs: ignored `*stage12c2a*`.

Lint identified duplicate strict decoders; factored the shared result/message
decoder while preserving each budget and error code, then reran focused output
tests and full HTTP/lint. CLI docs distinguish persisted, reserved, transport
accepted and uncertain outcomes. Native transport/reconciliation wiring and live
recipient consumption remain the next slice; these HTTP tests do not establish
delivery to a running provider or completion of stage 12.

## Stage 12c1 — typed messages and durable delivery reservations (2026-09-19)

Migration 0163 persists eight typed message kinds with immutable source task,
attempt, controller, criteria, context and configuration attribution. Messages
are bounded to 32 KiB, 256 per attempt and 10,000 per project; targets stay within
the project and replies retain thread/task scope. Attached results remain claims
owned by the source attempt. Source guards are shared with result submission.
The project timeline supports sender/recipient filtering and stable pagination.

The delivery journal reserves an exact target before native I/O, atomically with
audit/trigger CDC. Replaying a reservation cannot authorize another send. Only a
proven not_sent outcome permits retry, at most four times; dispatching/uncertain
records remain excluded from automatic retries after restart. handed_off is only
transport acceptance. Native send/reconciliation wiring is the next slice.

One domain and eight store message tests PASS, including concurrent submissions
and claims, malformed/foreign/thread/result references, immutable history, restart,
bounded attempts/project history, cancellation/expiry/termination/native guards,
audit failure rollback and no planning or lease changes. Existing result tests
and populated migration/downgrade regression PASS (focused store 1.868s, SQLite
0.957s). Full domain, SQLite (39.182s), store (21.875s), CDC, task/context suites and
backend build PASS. Final affected full rerun PASS (store 15.069s); pinned lint
across domain/ports/SQLite PASS, 0 issues. sqlc generation and diff checks PASS.
Logs: ignored `*stage12c1*`. No native/provider or desktop validation is claimed.

Tests caught mixed named/positional sqlc parameters in paging; all parameters in
that query now use names and generated code was regenerated. A fixture referenced
a nonexistent session-number field and tried to rewind the guarded heartbeat;
fixtures now use normal session allocation and explicit expired database facts.
Lint-required exported comments and a shadowed builtin name were corrected, then
the full affected tests/lint rerun. Review bounded resolution reasons so the audit
reason plus both IDs remains within the existing 2,000-byte vocabulary.

## Stage 12b — worker-result service, API and CLI (2026-09-19)

The shared service derives author, attempt and activation from the current native
session. Strict 512 KiB submission requests supply a native generation, retry key,
expected version and bounded claims; scoped history reads retain exact corrections.
Three daemon routes and HTTP-only CLI commands share this boundary. Generated
OpenAPI/TypeScript contracts, route redaction and CLI documentation accompany it.

Real SQLite HTTP tests cover first submission, replay, correction, conflicting keys,
paging, immutable history, unrelated task scope, malformed/forged/oversized input
and stale native generations. A completed claim leaves the task working with its
lease intact for independent evaluation. Service tests cover TUI/Chat attribution
and storage errors; CLI tests cover usage, bounds and daemon request-ID envelopes.
Focused tests PASS; full CLI (23.721s), task, telemetry, router/spec/envelope PASS.
Full controllers retain exactly the two recorded Windows pairing/clone failures.
Frontend client tests PASS (39), frontend typecheck, backend build, API generation
and changed-code pinned lint PASS (0 issues). Semantic API comparison finds zero
changes to pre-existing paths/schemas. Logs: ignored `*stage12b*`.

The HTTP fixture initially used an obsolete field and omitted its expected native
permissions; corrected before the focused and full checks. The final typecheck was
rerun after the earlier process result was lost at compaction, and exited zero.
This is protocol/API validation; native worker instructions, durable typed message
delivery, desktop inspection and independent evaluation remain subsequent slices.

## Stage 12a — structured worker-result persistence (2026-09-19)

Migration 0162 retains bounded schema-v1 worker claims: summary, implementation,
decisions, assumptions, interface contracts/files, reported tests, findings,
unresolved issues, follow-up recommendations and knowledge candidates. Commands
and claimed commit/outcome remain inert claims. Definitions are limited to 256 KiB,
16 immutable corrections per attempt, explicit collection/string bounds and portable
file references. Submission keys are idempotent; expected versions fence competing
corrections. Native generation/session ownership, sealed context, fixed criteria and
effective configuration activation are checked in the same transaction as result
and audit/trigger CDC. Pending native/configuration work and uncertain interface
recovery reject new claims; confirmed closed daemon recovery remains compatible.

Two domain tests and five store tests PASS, covering bounds, stale/unowned/unsealed
submissions, concurrent retries/corrections, cancellation/output retention, restart,
post-termination historical acknowledgement, immutable history, audit/CDC rollback,
the 16-version cap and configuration attribution after interface change. Results
neither release leases nor rewrite criteria, declare success or canonize knowledge.
The populated migration test exercises result/context foreign keys and downgrade.
Full domain/SQLite/store/CDC/context/task suites PASS (initial SQLite 32.232s, store
15.128s; final affected store rerun 12.053s). Backend build, sqlc generation and full
domain/ports/SQLite pinned lint PASS (0 issues). Logs: ignored `*stage12a*`.

Review added explicit interface/native-control fences and preserved the existing
reconciler's DAEMON_RESTARTED distinction. A test compile used the wrong recovery
constant name; corrected to the existing domain constant before final suites.
Result service/API/CLI, typed delivery and independent evaluation remain subsequent
slices. No live provider or UI validation is claimed for this storage increment.

## Stage 11c3 — context inspection API and CLI (2026-09-19)

`GET /tasks/{taskId}/attempts/{attemptId}/context` and `ao task context` expose
the sealed input and provenance without rebuilding, reading files or launching.
The service verifies that the attempt belongs to the requested task; missing
context has an explicit 404 while storage failures remain failures. Generated
OpenAPI/TypeScript contracts and telemetry route/command classification accompany
the new endpoint. CLI docs describe file selection, knowledge relevance and budgets.

Focused HTTP/CLI/service/telemetry tests PASS, including unrelated/missing attempts,
exact response preservation, usage errors and daemon error/request-ID envelopes.
Full CLI (23.875s), task service, telemetry, HTTP router/spec/envelope suites PASS;
full controllers retain only the two recorded pairing/clone Windows failures.
Frontend client tests PASS (39), frontend typecheck, backend build and changed-code
pinned lint PASS (0 issues). API generation PASS; semantic comparison against HEAD
found zero changes to pre-existing paths or schemas. Logs: ignored `*stage11c3*`.
The initial test compile used the wrong envelope type name; corrected to the
existing APIError and all affected checks rerun. Desktop context/knowledge UI and
live provider validation remain later stages, not established by these API tests.

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
