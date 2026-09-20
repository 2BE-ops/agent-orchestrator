# Adaptive agent platform: stage 15+ implementation review

Recorded 2026-09-20 as the stage-26 whole-diff review. Scope: the work landed
by stages 15 through 26 — the Agent Manager selection/composition/authoring
core (15), the shared scheduler (16), the orchestrator protocol and loop (17),
outcome attribution (18), evolution (19), project controls (20), the desktop
surfaces (21–22), failure/recovery integration (23), the full validation
matrix (24), the live Electron lab (25) and the upstream reconciliation (26).
Per-stage evidence lives in `adaptive-agent-platform-checklist.md` and
`adaptive-agent-platform-validation.md`; this document reviews the
architecture those stages produced, thematically rather than chronologically,
against the merged upstream head (`de5fcedac`).

## What was actually built

Eleven stages added 36 SQLite migrations (0148–0183), roughly 88k lines across
577 backend files (231 storage, 83 service, 76 domain, 55 httpd, 45 cli,
30 ports, 24 session_manager), and 38+ renderer components riding the
generated typed client. The functional arc: durable task graphs with exclusive
leases and atomic dispatch; sealed task contexts/results/messages/reviews;
attributable performance cohorts; persistent Agent Manager sessions with
governed admission, sealed routing decisions and native authoring; an
orchestrator goal/plan/complete protocol with deterministic completion;
experiments and recommendations with promotion gates; project-level controls
with storage-enforced fencing; recovery journals for notices, replacement
generations and reviewer reconciliation; and read-only desktop surfaces for
all of it.

## Theme 1 — Durability model: sealed facts, derived reads

The strongest and most consistent property of the implementation. Everything
a stage writes is an immutable, trigger-guarded fact: immutability and
retention triggers on every adaptive table, scope triggers that require the
exact project/task/attempt before an insert, echo/one-transition triggers on
state machines (needs-human pending→resolved, experiment conclusion, control
transitions), and history-preserving `Down` migrations. Nothing derived is
ever stored — task state, feedback phases, routing fates, drain-effective
paused state and attribution summaries are all projections computed inside
bounded, locked read transactions. Restart stability is a consequence, not a
feature bolted on: reopen tests across the storage suite prove the same
answers after close/reopen, and the stage-25 live lab proved it across real
daemon replacement.

The cost is honest to name: the store layer is large (231 files), and the
read-time projections (routing outcomes/summaries, cohort aggregation) walk
durable rows on every read. The bounds are real (366-day windows, 1000-member
cohorts counted in-transaction, cursor-paged sequences) but there is no
indexing strategy for the aggregate paths yet; at the current scale (bounded
windows, single-writer SQLite) this is correct and predictable, and it should
be watched, not regretted.

## Theme 2 — Enforcement chokepoints: one transaction, all callers

Stage 16's decision is the load-bearing one: admission (daemon-wide
concurrent-worker cap from `app_settings`, per-Agent-Type `maxParallelWorkers`
from the pinned `WorkerConfiguration` snapshot) is enforced **inside** the
session-creation transaction — `createSessionRow` — so all six launch callers
(HTTP spawn, session service, delegation, review launcher, Manager controller
admission, tracker observer) pass one gate under the single-writer lock with
no in-memory state to drift. Stage 20 then reused the same transaction for
control fencing (`ErrProjectAdmissionsFenced`) rather than adding a parallel
check, and dispatch replays were explicitly exempted so recovery can never be
blocked by its own fence. Typed 429 envelopes (`SCHEDULER_WORKER_LIMIT`,
`SCHEDULER_AGENT_TYPE_LIMIT`, `PROJECT_ADMISSIONS_FENCED`) surface through the
existing spawn error path — no new error plumbing. This is the right shape:
one chokepoint, durable counts, honest refusals.

"No blind reassignment" holds the same way: one live lease per task and one
dispatch per attempt are SQL-protected (0156/0157), attempt counts refuse at
reservation, and duplicate execution of an exclusive task remains
SQL-impossible rather than convention-impossible.

## Theme 3 — Native protocols: sealed, chained, golden-pinned

The Manager inbox/proposal/decision/authoring protocol family (v1→v4) and the
orchestrator goal/plan/complete/feedback protocol are rendered as immutable
chained byte sequences — each version renders **over the retained bytes of the
prior versions**, pinned by golden tests. Receipts are idempotent (exact
replay returns the original; changed payload conflicts). Identity is
daemon-minted, provenance is trigger-recorded, and the stage-15 registry
authoring path enforces per-kind quotas and a 64-action request bound. The
version accretion is deliberate — old sessions must read back their exact
protocol bytes forever — but it means every new capability adds a renderer
layer; v4 already composes four. Sustainable at this cadence, and worth
consolidating before v6.

The 25 lab exercised the honest-refusal property for free: a v1 Chat-mode
Agent Type was refused (native-terminal required) and the operator fixed it
through versioned authoring (v2 via New version + Use version) rather than an
in-place edit — exactly the immutable-version contract working as designed.

## Theme 4 — Attribution and evolution: evidence, not vibes

Routing and planning attribution (18) are read-time derivations coupling each
sealed decision/receipt to its task's derived fate, with summaries that count
complete bounded windows or refuse — never partial sums. Evolution (19)
derives comparable cohorts from stage-13 admission facts (mixed/unseeded
excluded with counters, dual-fed tasks counted as confounds), recomputes
evidence inside the write transaction rather than trusting caller evidence,
and gates `promote_candidate` on minimum samples plus the manager-versioning
policy. The recurring discipline — recompute, don't trust; bound, don't
sample; refuse, don't approximate — is applied identically across stages 17,
18 and 19, which is what makes the numbers mean something.

## Theme 5 — Recovery: journals and the never-unknown-is-dead rule

Stage 23 is where the durability model pays off. The notice journal (0183)
gives at-most-once cross-restart delivery with uncertain-vs-failed settlement
(startup marks interrupted pendings uncertain, never resends); replacement
generation journaling binds the new delegation generation to a unique
execution receipt so a stale identity cannot refresh sealed context;
reviewer reconciliation keeps a run on **unknown** liveness and refuses
substitution — the "never treat a failed/unknown probe as proof of death"
rule from AGENTS.md, enforced in code and tested. The stage-25 live kill
(mid-`waiting_input`, daemon hard-killed, surviving terminal delivering
session-end to the relaunched daemon) demonstrated the whole chain in the
real app.

## Theme 6 — Desktop surfaces: thin, typed, read-mostly

Stages 21–22 deliberately kept the renderer a supervisor surface: every view
is a read over the generated typed client, with the two contract repairs
(query containers missing from the generated spec) fixed at the source DTO
layer rather than by casting in the frontend. The ControlCenter is the only
write-heavy view and it offers exactly the transitions the stored state
machine accepts, confirms destructive actions, and reports bulk cancellation
honestly (cancelled counts, retained leases, per-session errors). The
stage-24 bisect catch (WorkerSelectionPanel crashing the whole dialog on a
malformed registry page) and its fix set the pattern: defensive flattens at
the boundary, component tests on both happy and malformed shapes.

## Theme 7 — Upstream coexistence

The stage-26 merge (28 upstream commits, zero migration collisions) proved
the branch's seams are where they should be: 20 of 25 overlapping files
auto-merged, and the only semantic conflict in the entire merge was
TaskComposer — the one place upstream's composer UX and the branch's registry
worker selection genuinely intertwine. The resolution kept both intents
(workerSelection guards + upstream's effort-preselect semantics) and the
combined test file (37/37) now pins the intersection. Everything else —
daemon wiring, controllers, chat service, session manager — composed without
hand-holding, which is the strongest evidence yet that the adaptive work
extended seams rather than rewiring shared paths.

## Carried gaps (recorded, not hidden)

- opencode advertises no `permissions` field in any mode, so registry-type
  opencode launches fail resolved-`auto` validation (upstream-class gap; the
  manual delegate path is unaffected). Same class the fake harness had before
  `f52471054`.
- The knowledge UI has no visible sources editor; the daemon provenance gate
  (1–16 sources) fires correctly and blocks creation. API-level authoring
  works.
- Live runs still owed: orchestrator goal/planning loop and Manager admission
  in the real app, native Manager experiment/recommendation tools (stage 19
  deferred them), and a concurrent heterogeneous live demo.
- Local live validation is Windows-only; race coverage exists only on CI
  (no local C compiler). Windows-environmental test baselines are enumerated
  and proven pre-existing, but they are noise every local run pays.
- The stage-25 lab artifacts (`.cache/ao-lab-25`, scratch data) are still on
  disk by design; they are not committed.

## Verdict

The implementation is coherent and disciplined. One durability model (sealed
facts, derived reads, trigger-guarded transitions) is applied uniformly
across eleven stages; enforcement lives in single transactions rather than
call-site conventions; protocols version without rewriting history; the
desktop rides generated contracts; and every stage's evidence distinguishes
what was proven live from what was proven at suite level from what remains
open. The weaknesses are scale (store breadth, protocol accretion, unindexed
aggregate reads at current bounds) and a handful of honestly-recorded product
gaps — none architectural. Stage 27's Definition-of-Done report can be written
from real evidence, which is what twenty-six stages of this discipline were
for.
