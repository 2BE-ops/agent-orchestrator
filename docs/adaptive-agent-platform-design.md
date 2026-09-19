# Adaptive agent platform: audit and proposed architecture

Status: stage 05 registry persistence is implemented; runtime integration and
desktop functionality remain in later stages. See the live checklist for evidence.

Audited on 2026-09-18 against `684d6d67db004e180f7ac9c649ce92840491ea4d`
(`fix(session): bound spawn readiness and rollback (#5557)`). The local branch
was `main` and the worktree was clean before this work. See the
[implementation checklist](adaptive-agent-platform-checklist.md) and
[environment evidence](adaptive-agent-platform-validation.md). This design must
be committed before major implementation, as required by the assignment.

## Existing lifecycle and integration boundaries

The source, rather than descriptions of pending PRs, determines what is available.

1. `frontend/src/renderer/components/TaskComposer.tsx` submits local project work
   to `POST /api/v1/orchestrators/delegate`. Standalone and Cloud paths also exist;
   a desktop extension must not accidentally send local project IDs to Cloud.
   The CLI's `backend/internal/cli/spawn.go` sends a mirrored DTO to
   `POST /api/v1/sessions`.
2. Controllers in `backend/internal/httpd/controllers/sessions.go` and the
   orchestrator routes call `service/session`. `DelegateTask` in
   `service/session/delegation.go` currently creates a worker immediately, then
   asks the orchestrator to refine its title asynchronously. It does not create
   a durable task or wait for an orchestrator routing decision.
3. `session_manager.Manager.Spawn` resolves an explicit harness before the
   project role default. `effectiveAgentConfig` and `applySpawnAgentConfig`
   combine project, matching role, and per-spawn configuration. Role-specific
   model, effort, and mode values do not cross into a different harness;
   permissions still inherit. `AgentConfig` currently contains model, effort,
   mode, and permissions, not per-role MCP, plugins, or environment fields.
4. Readiness, model/effort validation, Chat preflight, and TUI prerequisites run
   before launch. `buildSpawnTexts` and `session_manager/prompt.go` construct
   standing instructions separately from the task brief and issue context.
5. The manager creates the session seed and isolated workspace through existing
   storage and workspace ports. Repository workers use Git worktrees;
   standalone workers use AO-owned plain directories. Existing rollback paths
   preserve unsafe-to-delete workspaces after failed startup.
6. TUI uses the agent adapter plus platform runtime; Chat uses the native
   controller through the Chat launcher. Chat has detached provider hosts,
   persistent conversations, controller generations, and restart adoption.
   A session has one committed interface at a time.
7. Lifecycle/reaper observations update durable session facts. SCM observers
   persist PR/check/comment facts. Review services keep per-harness reviews and
   per-commit review runs. Session read models derive status and Kanban columns.
8. SQLite triggers write `change_log`; CDC and SSE invalidate React queries.
   Electron owns daemon supervision and the worker-isolated browser. The typed
   renderer client is `frontend/src/renderer/lib/api-client.ts`; its types are
   generated into `frontend/src/api/schema.ts`.

**Admission must cover every launch entry point.** `DelegateTask` currently calls
the manager directly, while tracker intake calls the session service. A check
only in `service/session.Spawn` would not enforce limits consistently. Restore,
resume, agent switching, and reviewer launches also need explicit resource
accounting; none may gain a second reservation for the same live controller.

## Capability gap matrix

Existing means the underlying capability exists in this checkout, not that the
requested adaptive workflow has passed live validation. Partial means useful
primitives exist but the requested product behavior is incomplete.

| Capability | Existing | Partial | Missing | Implementation strategy |
| --- | ---: | ---: | ---: | --- |
| Worker lifecycle, rollback, resume | Yes | | | Reuse `session_manager`, lifecycle, reaper and services |
| Git/worktree and standalone isolation | Yes | | | Reuse workspace ports and session worktree records |
| Per-worker heterogeneous harnesses | Yes | | | Agent Types resolve to existing `SpawnConfig.Harness` |
| Model/effort/mode overrides | Yes | | | Reuse model catalogs, capability validation and config merge |
| Native CLI subscription authentication | Yes | | | Keep native login/setup and readiness flows |
| Reusable provider/connection references | | Yes | | Add non-secret bindings around adapter-supported configuration |
| Per-role environment/MCP/plugins | | Yes | | Project env and Chat MCP exist; extend supported launch/restore contracts |
| Machine-readable capabilities | | Yes | | Compose ConfigSpec, model catalogs, Chat capabilities and readiness |
| Agent Type registry and ownership | | | Yes | Immutable configuration versions plus mutable registry policy |
| Agent Type comparison/rollback/import | | | Yes | Compare versions, change active pointer, portable schema |
| Skills | | Yes | | Preserve native skill discovery; add versioned AO-authored definitions |
| Skill resources and composition | | Yes | | Materialize pinned content using supported native skill conventions |
| Orchestrator role/conversation | Yes | | | Retain existing session and project-scoped conversation |
| Persistent Agent Manager | | | Yes | Durable policy, decisions and controller binding; existing execution engine |
| Task DAG | | Yes | | Existing task UI launches sessions; add durable task intent linked to sessions |
| Acceptance criteria versions | | | Yes | Freeze criteria in task attempts; privileged audited revisions |
| Task leases and exclusive assignment | | | Yes | Transactional attempts, fencing and restart reconciliation |
| Scheduler and project controls | | | Yes | Deterministic shared admission plus persistent dispatch intents |
| Structured results/handoffs | | Yes | | Extend handoff/artifact patterns with task-result schemas |
| Worker communication | | Yes | | Existing `Send` delivery; add durable typed messages and idempotency |
| Project knowledge | | | Yes | Reviewable, versioned facts with provenance and invalidation |
| Context construction | | Yes | | Extend existing prompt construction with bounded selection and manifest |
| Independent reviews | | Yes | | Existing reviewer harnesses/runs; add task policy and version attribution |
| Objective evaluation | | Yes | | Aggregate existing SCM/review/runtime/usage evidence per attempt |
| Performance by version and skill | | | Yes | Query attributable evidence with sample sizes and confounders |
| Manager/orchestrator evaluation | | | Yes | Persist decisions and planning revisions with outcomes |
| Experiments/recommendations | | | Yes | Explicit cohorts, hypotheses, gates and policy-controlled promotion |
| Needs Human | | Yes | | Existing blocked/input states; add task-scoped reasons and resolution |
| Dry-run planning | | | Yes | Pure read-only plan/selection/admission simulation |
| Unified audit | | Yes | | Add durable actor/intent audit, delivered through existing CDC |
| Desktop management surfaces | | Yes | | Extend Task Composer, Session Inspector, settings and routing |
| Restart persistence | | Yes | | Existing session recovery plus adaptive state reconciliation |
| Fake harness testing | Yes | | | Reuse fake harness and injected drivers; no paid calls in normal tests |

### What will not be duplicated

- Sessions remain the worker instances. There is no second worker/process table.
- Existing Git, worktree, browser, runtime, controller, PR, review, conversation,
  notification and usage services retain ownership of their behavior.
- Existing project configuration remains the default for legacy launches.
- The built-in `using-ao` skill remains embedded and installed by `skillassets`.
  Native Codex/ACP skill discovery remains live-provider discovery through
  `ChatSkillLister`; it is not replaced by a stale database catalog.
- Existing `review` and `review_run` records remain review evidence. New
  evaluations reference them rather than copying review lifecycle logic.
- Agent adapters remain the only code that constructs provider commands.

## Proposed data model

Names below are design vocabulary, not claims that migrations have been written.
Use new goose migrations after the current last migration, `0147`, with numbers
rechecked after fetching upstream. Use sqlc sources and generated stores; do not
edit existing migrations or generated code manually.

Stage 05 storage realization: Agent Types and authored Skills share
`adaptive_registry` identity/policy rows and `adaptive_registry_versions`, with
a closed typed definition union. Kind-qualified foreign keys prevent cross-kind
active versions and Skill pins. `adaptive_registry_skill_pins` preserves exact
ordered composition, and `adaptive_registry_audit` retains semantic history
independently of CDC retention. This shares the identical ownership/versioning
mechanics while retaining distinct Agent Type and Skill domain content; native
provider skill discovery remains unchanged. Appending a version does not activate
it. Manager promotion remains denied until the explicit stage 19 approval gate.
Migrations 0148 (additive CDC vocabulary) and 0149 (registry) implement this slice.

| Record | Durable content and invariants |
| --- | --- |
| AgentType | Stable ID, name, description, origin, enabled flag, active version, owner policy, revision |
| AgentTypeVersion | Monotonic version, parent, immutable harness/config/instructions, content hash, creator, creation reason |
| Skill / SkillVersion | Stable registry identity and immutable instructions, bounded resources, capabilities and requirements |
| AgentTypeVersionSkill | Ordered references to exact Skill versions; never a floating latest-version attachment |
| ProviderBinding | Harness, non-secret native configuration reference, safe label, enabled state; no embedded credentials |
| AdaptiveProject | Opt-in, goal revision, optimization preference, limits, approvals and persisted control state |
| Task / TaskRevision | Project, brief, category, priority, parent, author, intent phase, retry policy and acceptance reference |
| TaskDependency | Same-project edges, unique pair, no self-edge or cycle; mutation and validation in one transaction |
| AcceptanceCriteriaVersion | Immutable requirements and evidence expectations; author, reason, predecessor |
| TaskAttempt | Task/criteria version, attempt number, existing session ID, lease generation and launch intent |
| WorkerConfiguration | Effective launch snapshot: Agent Type/Skill versions, overrides, binding metadata, context hash, decision |
| ContextSnapshot | Ordered inputs, selected content or immutable references, hashes, budget and omission reasons |
| ProjectKnowledge | Versioned content, kind, source, author, confidence, candidate/accepted/invalidated/superseded state, pin |
| WorkerArtifact | Attempt/session owner, schema version, kind, content reference/hash and validation result |
| AgentMessage | Typed sender/recipient/task references, correlation/reply ID, delivery state and deduplication key |
| Evaluation | Attempt, criteria version, target commit, evidence references, outcome and evaluator version |
| ManagerDecision | Requirement snapshot, candidates and exclusions, selection, reason, new-resource actions and eventual outcome |
| PlanningEvent | Goal/task revisions, late dependencies, cancellation/correction reasons and attributable actor |
| Recommendation | Observation with sample size, proposed version diff, disposition and decision actor |
| Experiment | Hypothesis, exact control/candidate versions, eligibility, allocation, minimum samples, outcome and promotion gate |
| AuditEvent | Ordered actor/action/resource/revision references, request/decision correlation and safe structured details |

Do not maintain a mutable universal performance score. Compute metrics from
attempts/evaluations and existing evidence, with caches only if measured query
cost justifies them. Retain version identities even when a definition is disabled.

Registry metadata/policy uses optimistic revisions. Configuration edits create
new immutable versions. Rollback selects an older version for future launches;
it never rewrites historical versions or running workers. A promotion or disable
action is an audited lifecycle event separate from the immutable configuration.
Skill deletion must not break past snapshots. Knowledge deletion removes it
from future context while past manifests retain an explicitly documented history
reference; the UI must distinguish deletion from erasing retained history.

## Services and launch integration

Add cohesive daemon services for registry, task coordination, context/knowledge,
evaluation and adaptive management. Keep domain records free of HTTP/SQL types.
Expose narrow storage and scheduler ports; wire them in `daemon`. Services must
not import concrete runtime or agent adapters.

The registry exposes the same create/version/disable/clone/import validation for
human and manager actions. The service receives an actor established by the
application action context; a submitted `origin: USER` is not authorization.
Manager selection, modification and versioning are separate permissions. A
blocked action becomes a recommendation or approval item. It cannot silently
modify user policy or promote itself to a human actor.

Preserve AO's trusted-local-host model and unauthenticated loopback listener.
Where existing session capabilities scope worker tools, retain that boundary.
Do not claim these policy checks contain a malicious process already able to
act as the local user. LAN routes retain the current bearer middleware and exact
identity exemption; this project needs no new network listener.

At launch, resolve the Agent Type, exact Skills, local provider binding, project
defaults and explicit one-off overrides into a frozen snapshot. Validate all
capabilities before reserving work. Model overrides must never be substituted
silently; switching to a different harness must not inherit incompatible model
aliases. Distinguish omitted values, explicit provider defaults and empty lists.
Use the existing permission vocabulary and validate its actual adapter mapping.

The scheduler supplies a durable dispatch identity to the existing session
manager. Associate the session seed with its reserved attempt atomically, before
any process starts. Retried dispatch looks up that identity instead of creating
another seed. This small extension is necessary: spawning first and binding the
task afterwards has a restart window that can create duplicate workers.

Expose a shared admission hook at the manager boundary to cover direct spawns,
delegation and tracker intake. It is disabled for legacy projects. An adaptive
reservation is consumed once; it is not counted again as an independent manual
spawn. Restore/resume account for existing process ownership before admission.
Reviewer and manager/orchestrator processes have explicit budgets and remain
visible even if they are not counted as implementation workers.

Adaptive workers restore their frozen configuration. The current restore path
still reads project defaults in some cases; only adaptive launches should gain
the new exact-version behavior initially. Later model/harness changes create a
new execution segment and provenance entry rather than rewriting launch facts.

## Providers, capabilities and Skills

`service/agent` already supplies installation/auth readiness and model catalogs.
`ports.ConfigSpec`, `AgentModelCatalog` and `ChatCapabilities` describe different
aspects of support; combine them into a mode-aware view rather than inventing a
second static supported-harness list. Unknown, unavailable and unsupported are
distinct states. Check both definition validity and current launch readiness.

`service/agentauth` launches fixed native login/setup commands, not arbitrary
client commands. Codex account switching is currently device-global; a type
must not advertise independent simultaneous Codex accounts until an adapter
actually supports isolated account binding. Agent Types reference local auth
configuration without copying credential contents into records, prompts, logs,
portable exports or audit events.

Custom providers use existing native configuration and configured model catalogs
where supported. An explicit provider reference must fail visibly if missing or
incompatible. Revalidate bindings on launch and distinguish active-host adoption
from a fresh launch after credentials were removed. Do not convert native CLI
subscription operation to direct model API requests.

Chat setup already accepts `ChatMCPServerConfig`, and live providers advertise
MCP reload/config controls. That does not establish a general TUI plugin/MCP
configuration feature. Gate controls by verified adapter and mode support, then
implement launch and restore together. Do not store MCP secrets inside generic
configuration JSON; use native secret storage/references at the adapter boundary.

AO-authored Skills add immutable authoring and composition around the existing
skill conventions. Persist resources with size/type/path limits and content
hashes. Materialize pinned versions into an AO-owned session location; never
overwrite native user skills or the embedded `using-ao` asset. Imported native
skills become an explicit snapshot, not a silently synchronized second owner.
Reject traversal, symlink escapes and executable/configuration requirements that
the selected harness cannot honor. A tool requirement is not a permission grant.

Import/export uses a schema version and an allowlist of portable fields. Local
provider and secret references require rebinding after import. Import must not
install plugins, execute resources, alter authentication, or enable automatic
management as a side effect. Display unresolved requirements before launch.

Stage 07 realization: portable schema v1 embeds the content of each pinned Skill
in composition order, rather than exporting local IDs. Import creates fresh
identities and v1 snapshots atomically, disabled with all manager permissions
off. Bundles are bounded to 1 MiB and 32 dependencies. Local binding IDs become a
`providerBindingRequired` fact, never a credential or account reference; the
capability/launch stages enforce rebinding. The desktop previews requirements
before importing and supports resource authoring, JSON export and file import.
Native resource materialization belongs to the immutable launch snapshot path
in stage 09 so authoring/import cannot change native user skill directories.

## Tasks, scheduling, leases and controls

Tasks record work intent independently of a session's derived display status.
A task may exist without a worker and may have multiple historical attempts.
Use persisted intent/execution facts to derive planned, blocked, ready, leased,
working, review, failed, completed, needs-human and cancelled presentation.
Do not add those presentation labels to the session storage model.

Before the first lease, pin acceptance criteria and dependency/task revisions.
Implementing workers can submit evidence or propose revisions but cannot revise
their own criteria. User/orchestrator revisions require a reason and preserve
the old attempt's criteria; re-evaluation or a new attempt is explicit.

Admission checks dependencies, cancelled/needs-human ancestry, priority, retries,
global/project/type limits, supported capabilities, current readiness, observed
capacity and project controls in a transaction. Include already-running workers
in limits. Profiles, experimental versions, task depth, pending tasks and active
experiments have independent enforced bounds. No LLM action bypasses admission.

Lease heartbeat and last meaningful activity are separate facts. A lease timeout
triggers reconciliation; it is not proof that a detached host died. Reconnect
through the existing manager, fence old generations and establish confirmed
termination before exclusive reassignment. An inconclusive probe keeps the
reservation and presents recovery/Needs Human. Do not force-delete dirty trees.

Persist dispatch, message delivery and evaluator cursors so restart resumes work
idempotently. Test crashes before/after reservation, seed insertion, process
creation, result write and evaluation delivery. Reconcile reservations against
session ownership before opening admission on daemon startup.

The task launch/restore boundary also reserves native side effects durably
(0157). A pending execution prevents releasing or transferring the task lease,
including when the session row was marked terminated during incomplete cleanup.
Its ID is the reserved native target generation passed to the existing
controller/supervisor; retain the source owner for reconnect/compensation checks.
Only verified connection or termination resolves it. An unknown result survives
restart for reconciliation. A SQLite resurrection fence rejects restoring a
released historical task worker, closing the restore-versus-reassignment race.

Task run/cancel instructions use a separate immutable history and optimistic
version fence (0158), retaining the planning revision that the actor saw.
Cancellation closes reservation/seed/native-operation admission for the current
parent chain. Active leases and unresolved native operations remain retained;
the read projection shows cancelling until lifecycle cleanup confirms release.
Explicitly resuming an ancestor does not erase a child's own cancellation.

| Control | Deterministic behavior |
| --- | --- |
| Pause | Fence new admissions immediately; current tasks continue; retain pending work |
| Resume | Reconcile ownership and resume eligible pending admissions |
| Drain | Fence admissions; let current attempts finish; release their workers through normal lifecycle; become paused |
| Stop after current tasks | Fence admissions and follow-up dispatch; finish current attempts, stop adaptive controllers, become stopped |
| Cancel pending | Cancel unleased work and pending launch intents; reconcile reserved-but-not-started attempts |
| Cancel all | Cancel pending work, fence dispatch generations, terminate owned active workers through normal kill services; persist incomplete cancellation for recovery |

Controls target the adaptive project and its owned processes, with affected
workers shown in the UI. They do not act on unrelated projects. A new task cannot
slip through concurrently with pause/cancel. Cancellation retains workspace
cleanup protections and reports partial failures rather than declaring success.

Needs Human has structured reason, provenance and resolution. Missing credentials
or approval blocks only the affected task and its descendants. Other eligible
branches remain schedulable. Workers with a live permission prompt retain AO's
existing blocked-input protection; automation never injects an approval response.

## Context, knowledge, artifacts and communication

The user's 2026-09-19 amendment adds pre-slice 15.0 (one migration plus domain
and store) and stage-15 integration. Sensitivity is an ordered lattice:
`technical` < `engagement` < `mission`. Tasks and ProjectKnowledge entries carry
classification; immutable Agent Type versions carry `max_context_class`
clearance. Engagement means per-client/per-target context shared only within
one engagement, so clearance alone does not authorize cross-engagement sharing.

Context Builder must deterministically refuse higher-class embedding in any
delegation payload, context manifest or knowledge selection. Authority comes
from the attempt's pinned Agent Type version/configuration, never the live Type.
Manifests retain the classification of each sealed item; delegation payloads
are versioned inspectable artifacts. Technical worker findings/results may flow
upward into higher-class context. Violation tests must prove these boundaries,
including live-version changes and cross-engagement isolation, at the same
level as ownership enforcement; prompts are not enforcement.

Stage 15 adds classification to Manager compatibility filtering and exposes
Type clearance, task classification and knowledge classification selectors in
their authoring views. The session inspector displays per-item classifications
from the sealed manifest. This amendment extends, rather than replaces, the
existing version, provenance, budget and ownership rules below.

Foundation encoding uses `classification` and project-local `engagementId` in
immutable task/knowledge definitions, and `maxContextClass` (the API spelling of
`max_context_class`) in immutable Agent Type definitions. Missing historical
fields resolve to technical without changing original JSON bytes or hashes.
Engagement classification requires a scope; mission content can retain a scope
when derived from engagement inputs. Technical task intent may identify its
receiving engagement; technical output remains eligible to flow upward.

Clearance is loaded from the exact registry version/hash in the attempt's initial
dispatch configuration. Active Type versions and configuration overrides cannot
raise it. Knowledge selection accepts an attempt identity, derives its frozen
relevance and scope, and applies class/scope filtering before the candidate limit.
Schema-v2 context seals record explicit item labels, aggregate sensitivity and
exact system instructions. Parent/dependency briefs, criteria and selected files
inherit their task revision's label. Results/interface contracts inherit the
source attempt's sealed aggregate, so a technical task that received mission
knowledge cannot turn its output into technical input. Worker knowledge candidates
must retain at least that source sensitivity and engagement.

Migration 0173 adds definition validation and immutable, numbered delegation
artifacts. Initial artifact version 1 seals the exact system/task text, context
hash, configuration hash, class/scope and native execution identity in the same
transaction as the manifest. It is an input receipt, not a delivery acknowledgement.
Legacy manifests stay readable without fabricating missing system text or receipts.
Forbidden optional sources contribute only a generic omission explanation, without
their identifiers/content/hashes. Store validation independently verifies source
labels, even on omission metadata. Stage 15 still connects public artifact history,
Manager selection/delivery, live message/review context gates and authoring/inspector
UI; stage 23 must retain new execution receipts when restoring a native generation.

Extend `buildTaskPrompt`/`buildSystemPromptText`; do not assemble business logic in
React. Context selection includes the pinned task/criteria, parent summary,
completed dependency contracts, selected accepted knowledge, relevant file
snapshots, pinned Skills and Agent Type instructions. Bound total bytes/tokens
and each source; record selection/omission reasons and hashes. File reads remain
inside validated repository/workspace paths. Do not fetch arbitrary remote
resources automatically during context construction.

Knowledge candidates remain untrusted until accepted by permitted policy. Record
source worker/artifact/commit, timestamps and supersession. Contradictory facts
are visible, not silently merged into an opaque summary. Invalidation affects
future contexts; already-launched workers retain their recorded snapshot.

Knowledge revisions retain content and review disposition together (0159–0160).
Pins require accepted status; supersession names an exact accepted replacement
in the same project. Soft deletion removes current default selection while
retaining historical versions for manifests and audit. Worker and orchestrator
proposals are candidate-only until a permitted review/promotion policy applies;
workers must attribute an unreleased owned attempt and cannot rewrite history.
Human/system revisions use an expected-version fence. Context reads accepted
versions explicitly, never merely the latest candidate or a confidence label.

The task context manifest is a separate immutable attempt/session record, sealed
after workspace provisioning and before native launch. It references the original
worker-configuration hash instead of rewriting that already-committed snapshot.
This lets file provenance describe the actual worker workspace. It retains the
rendered task prompt, exact task/criteria/dependency/knowledge versions, Type/Skill
references, source selection/omission reasons and the rendered system-prompt hash.
The native execution reservation remains held across construction and launch.
Restore reads retained prompt content; it does not reconstruct historical context
from current knowledge. Inline prompt bytes and estimated tokens are bounded;
materialized Skill resources remain exact references, not eagerly inlined files.

Worker results use a versioned schema containing summary, implementation,
decisions, assumptions, interfaces, tests, findings, unresolved issues,
follow-ups and knowledge candidates. Reject malformed/oversized submissions
without losing partial worker output. Self-reported tests are claims, not
independently verified evidence.

Stage 12 result realization uses immutable schema-v1 worker claims, at most 16
corrections per attempt and 256 KiB per definition. Each submission has a durable
idempotency key and expected result version. The daemon supplies session/controller
ownership and configuration activation fences; storage records the frozen task,
criteria and context alongside the effective configuration at submission. A result
cannot release a lease, rewrite acceptance or promote its knowledge candidates.
Cancellation/expiry alone does not discard output from the still-owned controller;
termination, replacement and unresolved native/configuration changes reject new
claims. Exact retries acknowledge historical content without new effects. Existing
closed interface recovery (`DAEMON_RESTARTED`) is distinct from uncertain recovery.

Typed messages cover finding, question, answer, blocker, handoff,
interface_contract, review_request and dependency_update. Validate sender/target
project and attempt ownership; persist first, then deliver via existing
mode-aware `Send`/Chat delivery. Correlation IDs and delivery keys prevent
duplicate effects after restart. The orchestrator observes the same timeline.

Stage 12 typed-message realization retains schema-v1 messages up to 32 KiB, 256
per source attempt and 10,000 per project. A message targets another task in the
same project, including one without a worker yet; replies retain the thread and
reverse the original task pair. Answers reference questions. Attached results
must belong to the sending attempt. Source attribution uses the same transactional
controller/context/configuration checks as result submission. Exact retries only
acknowledge retained content. Messages do not modify planning or acceptance.

Migration 0163 stores immutable messages separately from bounded native delivery
attempts. The daemon reserves the current target attempt/session/controller before
I/O and may send only on a newly created claim. A replay never authorizes another
send. Only a proven `not_sent` outcome allows retry (at most four attempts); a
`dispatching` record surviving restart or an `uncertain` outcome requires
reconciliation. `handed_off` means transport acceptance, not agent acknowledgement.
The existing TUI paste/Enter transport cannot prove recipient receipt; neither
elapsed time nor daemon restart may be treated as evidence to repeat that write.
Stable message delivery keys also feed the existing Chat idempotency boundary.

Stage 12 native delivery waits for startup session/runtime reconciliation before
scanning the outbox. Migration 0164 checkpoints a rotating sequence cursor so
temporarily blocked recipients do not starve unrelated messages across restart.
Each five-second cycle examines at most 16 candidates, with a ten-second native
deadline per candidate and a separate three-second outcome commit budget. Native
readiness failures do not consume delivery attempts or prove worker death. A
recipient with an unresolved/uncertain prior send rejects additional reservations
until reconciliation, preventing a later automated paste from appending to an
unknown partial write. Human resolution integrates with stage 20.

TUI writes require an idle, input-ready, exactly owned live runtime, and recheck
generation at the existing guarded coordination boundary while holding the
session operation fence. Chat uses the existing keyed automation relay and queues
through its native controller. Worker-provided message JSON is escaped and labeled
as claims; it grants no change to planning, criteria, permissions or ownership.

Stage 12 context enrichment selects the latest correction from at most three
prior attempts and the latest result for each reserved dependency revision.
Projected findings, unresolved issues and interface claims retain the original
result hash and exact projection hash; executable test commands are excluded.
Up to eight incoming interface proposals carry their immutable message provenance.
Candidate and prompt limits record explicit omissions. Sealing rechecks task/
dependency ownership, revision pins and exact content. These historical worker
claims confer neither verified completion nor delivery acknowledgement; later
messages and result corrections never rewrite a sealed context.

The initial native output protocol is included in the sealed base prompt, within
the same byte/token budget. It records literal executable/argv, the owning run-file
environment, request examples and the reserved generation. Stale workers may not
query the current session to replace that generation. The recovery integration
must journal and deliver replacement-generation instructions without rewriting
historical context, including when Chat adopts a surviving native host. This is a
native ownership transition, not permission for a worker to self-refresh identity.

## Evaluation, management and evolution

Evaluations reference exact criteria and target commits. Gather objective facts
from existing PR checks, SCM/review runs, usage and lifecycle records. New local
verification must use a bounded execution boundary and preserve command, exit
code, timing and output provenance; text that says 'tests passed' is insufficient.
Unknown/pending/stale-head evidence never becomes a passing result. Qualitative
review supplements deterministic evidence and is visibly labeled.

Independent review policies compare implementing and reviewing Agent Type
identities/versions and, where requested, harnesses. Existing reviewer execution
is reused, with added configuration attribution; a reviewer is not a duplicate
implementation worker. Distinguish a review finding, crash, environment failure,
planning mistake and human rejection in outcomes.

Stage 13c3 freezes the reviewer policy in acceptance criteria, with an exact Type
version and optional different-Type/harness requirements. Migration 0167 extends
existing review runs with a scope hash and a separate immutable context record;
it does not introduce another review lifecycle. Context retains frozen criteria,
result/commit, implementing configuration and full reviewer configuration.
Insertion rechecks result freshness, PR head/ownership, registry availability and
provenance in the same transaction as the run and task audit. Context is capped
at 2 MiB and review history at 64 passes per result, including failed retries.
A native launch witness can be recorded once against the matching reviewer
launch ID and handle. Mere preparation is not evidence of launch or acceptance.
Repeated invocations share an effective scope while retaining their own hashes;
ordinary review scope remains separate. Native consumption, generation-fenced
submission and evaluation attribution must use this retained context.

The shared task service now requests native review from an exact result ID;
callers cannot supply reviewer configuration, criteria, authority or verdict.
It resolves the frozen policy through the existing registry, seals its output,
and invokes the reviewer service behind the Codex account-operation gate. The
store rechecks explicit Type settings and selection authority before insertion;
orchestrator selection obeys Manager select policy. HTTP/CLI expose bounded
per-result history and individual sealed context inspection. Retried admission
still checks current Type/native availability, while historical inspection never
re-resolves registry content. Generation-fenced submissions acknowledge exact
historical retries and atomically retain the review audit. Uncertain native
launch returns a reconciliation conflict with its running pass retained.

Review evaluation now collects the latest native pass per PR/harness for the
exact result, separately from generic review history. It decodes and verifies
retained context in the evaluation transaction, then stores compact result,
criteria, implementing/reviewing Type/configuration and launch attribution.
The immutable context hash links to full Skill content without multiplying it
across every PR/evaluation. A review criterion requires observed current PR heads,
matching frozen policy and result provenance, and a native launch witness.
Incomplete/failed native passes remain inconclusive; a changes-requested verdict
fails only the qualitative review criterion. Generic approvals and prior-result
reviews cannot pass it. Newer passes supersede older approvals for assessment.
Qualitative review does not supply missing CI, artifact or mergeability evidence.

Current completion is a transactionally derived read, separate from historical
evaluation outcomes. It requires the latest attempt/result/assessment at the
current task revision, a retained passing assessment, no cancellation intent,
and still-passing current SCM/review evidence. Assessed PRs must still have an
observed matching head. Verified immutable Git blob observations may be reused
without filesystem I/O in SQLite. Task reads expose the result/assessment IDs and
reason; completed tasks keep active lease ownership until native lifecycle
reconciliation releases it. A new attempt, result or revision supersedes the old
completion projection. Shared scheduler dependency/admission checks in stage 16
must use the same predicate inside their reservation transaction, not trust a
prior API read. Historical performance must distinguish assessed success from
this current completion projection.

Performance cohorts use attempt admission time, with outcomes and native usage
observed at read time. Each attempt is one denominator unit even if it has many
result revisions or evaluations. Unseeded reservations remain visible without
invented configuration attribution. The configuration is the latest result's
exact pinned configuration, or the original launch when no result exists;
activation history flags mixed attempts, including subsequent rollbacks. Native
usage remains session-wide and cannot be assigned to one epoch in mixed work.
Missing counters remain null; known zero is distinct. Priced cost is a partial
sum when not every event is priced, with native/estimated/unknown event counts.
Integer overflow fails the read rather than returning partial or zero totals.
Historical CI failure counts attempts with a failed frozen criterion decision;
review changes count witnessed native review passes, not prose findings. First
pass means attempt one, result one and both first and latest assessments passed.
Elapsed reservation time is distinct from CPU/runtime duration. Bounded pages
retain evidence IDs, the admission window and the observation time.

Aggregate reads use one transaction and a complete cohort of at most 1000
attempts; larger windows fail explicitly, while paged evidence remains available.
Overall totals include every attempt. Type/Skill/version/harness/model groups
exclude mixed and unseeded attempts with distinct counts; category/capability
groups include them. Skill and capability memberships overlap, so group totals
are not a project denominator. Metrics retain known-usage samples, priced event
counts and closed-reservation duration samples. Retries count extra attempts
once, not the sum of prior attempt numbers. These observational dimensions are
not experiment allocation or causal comparisons (stage 19).

Stage 13's initial collector snapshots independently observed CI facts under the
same transaction as evaluation and audit. Migration 0165 preserves assessment,
frozen criteria/result/context hashes and the exact historical worker activation;
at most 64 assessments per attempt, with stable retry keys and expected versions.
Only the latest result can receive a new assessment; exact historical retries
remain acknowledgements. Outcomes do not themselves release native ownership.

CI criteria pin exact required check names before dispatch. Passing requires
success on the result commit/current PR head and complete snapshot membership.
Existing PR storage retains old check rows, so migration 0166 records each check's
observation timestamp and compares it with the parent CI snapshot timestamp.
Checks missing from a newer snapshot and legacy rows without provenance cannot
pass. These timestamps identify stored observations, not a claim of recent network
refresh. Relevant check collection is capped at 128 with explicit truncation;
incomplete collections cannot pass. New criterion fields use omitempty so prior
criteria and context hashes remain stable. Remaining evidence kinds are initially
inconclusive rather than accepted from a worker's claims.

The shared task service exposes strict evaluation requests and scoped historical
reads. URL task/attempt scope is checked before collection, and the store repeats
transactional result/version/actor guards. The public request contains no verdict,
evidence, criteria or actor fields; exact retries acknowledge the retained snapshot.
HTTP/CLI collection reads stored SCM facts and explicitly supported local evidence,
without refreshing SCM or starting workers.

Evaluation observations additionally retain bounded PR metadata, latest existing
review runs per PR/harness at the target commit, and worker/lease facts under the
same transaction. Reviewer prose is limited in SQL before loading and then to a
4 KiB UTF-8 preview, with preview hash/original length. No reviewer Type identity
is inferred from a harness, and generic qualitative approval does not satisfy
task-specific criteria. Frozen mergeability criteria use exact observed PR heads;
unknown/draft/stale/truncated facts never pass. Reservation time is labeled as
ongoing until release, not billed execution time or proof of a crash. These fields
are optional on old snapshots to preserve historical hashes.

Commit-bound artifact collection is a daemon adapter, not worker output. Frozen
artifact criteria may specify a portable path and expected SHA-256. The shared
service authorizes a preparation/read and checks exact retry history before Git
access, then persistence repeats current-result/version/actor fences. Collectors
run outside SQLite transactions. The adapter uses raw Git tree/blob reads with
replacement objects, lazy fetching, inherited Git environment and fsmonitor
disabled; no working-tree/export filters, content execution or repository writes.
At most 16 regular blobs of 1 MiB each are read within 15 seconds. Missing paths
or mismatched hashes fail; missing commits, unsupported entries and unavailable
sources remain inconclusive. Internal collected evidence is excluded from request
JSON and validated against exact frozen criterion IDs/paths/commit before storage.
Subprocess output bounds cover io.Copy fast paths as well as direct Write calls.

Build/test/lint evidence reuses the existing SCM check collector: those criterion
kinds may freeze exact check names, with the same commit/head/snapshot guards as
CI. This follows mission §§24/28 and the reuse decision above; a separate daemon
runner for arbitrary project commands is not required to collect these outcomes.
Command vectors remain inert legacy expectations and cannot be combined with check
selectors or accepted as proof. The earlier ledger's proposed new command runner
is replaced by this existing evidence boundary. Recorded CI status is not labeled
as a locally executed command, exit code or locally measured duration.

Manager selection first filters permitted, enabled, compatible types, then
considers existing Skills before proposing evolution or new types. Persist
candidates, rejection reasons, selected versions, policy preference, and rationale.
No deterministic tag matcher is presented as semantic LLM planning. Provide a
planner/selector boundary with scripted test doubles and a production controller
using AO's configured native harness. Malformed proposals remain unapplied with
bounded retry or Needs Human; all accepted proposals pass the same services.

Stage 15a adds a shared registry candidate assessor and request-scoped API/CLI.
It checks exact Type clearance, enabled state, independent selection permission,
pinned Skill permissions and explicit required capabilities before native probes.
Capability strings are prerequisites, not semantic ranking. Compatible project
defaults and native checks reuse worker launch's resolver/checker. Results retain
Type/Skill content hashes and metadata revisions, effective native choices,
provider revision and catalog fingerprint, with application-owned exclusion codes
instead of raw native diagnostics. Pages include rejected Types and use a lookahead
cursor over the complete registry, with at most 20 native checks per request.
Explicit historical versions remain assessable without following activation.
These are live observations, not saved decisions or launch authorization; the
decision/application transaction must recheck their policy/content pins. Native
protocol v2 supplies scoped candidate tools while preserving v1 input bytes.

Persist manager policy/status, its controller/conversation binding, work inbox and
decisions. Reuse the conversation/runtime execution engine. Decide any required
session-kind extension explicitly with migrations and lifecycle tests; do not
hide a manager among ordinary implementation workers or reimplement processes.
The orchestrator continues to own goal decomposition and calls structured task
and delegation capabilities. Its instructions reference those capabilities, not
provider-specific CLI invocation strings.

Stage 14 chooses a distinct `agent_manager` session kind, one active controller
per project. Its native Chat narrative uses the existing session-scoped
conversation path, separate from the orchestrator's project-scoped narrative.
Migration 0168 widens the exact session-kind constraint transactionally without
rebuilding the heavily referenced sessions table (the established 0140 approach),
retaining CDC and foreign keys. Manager kind/project identity is immutable;
projectless managers and concurrent active manager seeds are rejected. Downgrade
refuses retained manager history instead of relabeling it. The partial unique
index is a final consistency guard, not evidence that an uncertain native owner
has exited. Durable manager admission/reconciliation must precede replacement.
Generic session API/service spawn cannot create this role. Stage 14's subsequent
policy, inbox and native controller slices provide its dedicated admission path
and restore behavior using existing session/conversation engines.

Stage 14a2 separates desired Manager governance from native ownership. A project
has immutable numbered configurations containing an exact controller Agent Type
version, a sealed historical Type reference, bounded optimization/creation/inbox
policy and user provenance. Configuration edits use compare-and-swap and retain
audit/CDC atomically; they never launch a session or rewrite a running controller.
Only human authority can change governing policy. Creation is opt-in and remains
subject to both these quotas and each registry entry's independent permissions
when actions are implemented. A disabled Type can still be referenced when
disabling the Manager; retained history remains inspectable after registry edits.
Migration 0169 refuses downgrade over retained policy history. Native controller
admission will resolve the exact pinned Type through existing worker configuration
and session engines rather than introducing another provider execution engine.

Stage 14b1 adds durable controller admission, atomic session/configuration binding
and native execution intent/resolution (0170). One unreleased admission per project
survives timeout and daemon restart; retained history is bounded to 1000 admissions.
Only user/system service context can reserve against enabled, current governance.
The dedicated seed transaction rechecks the exact Type, rejects ungoverned overrides
and retains the existing WorkerConfiguration format, including pinned Skills.
Ordinary configured session creation stays worker-only. Repeated reserve/seed/Begin
calls are inspection acknowledgements, never permission for another native launch.

Each dispatch/restore reserves its target native generation before side effects.
Unresolved operations block both release and replacement, even when the session's
terminated flag has changed. Resolution fences the observed owner; connection also
requires the reserved generation or exact existing Chat adoption. Lifecycle code
must independently confirm native connection/termination; unknown probes do not
provide evidence. Bound admission release requires that exact terminated owner
and no pending operation. Unseeded intent can be cancelled safely. A database
restore guard prevents resurrection after release. Audit/CDC share every mutation;
downgrade refuses retained admission history. Production lifecycle hooks and the
Manager inbox are subsequent stage-14 slices, not supplied by these store methods.

Stage 14b2 connects those intents to the existing TUI and Chat engines. A dedicated
internal admission is mandatory before Manager spawning; replay returns the
retained session before live readiness or registry changes can cause new effects.
Launch and restore reserve a native generation and resolve only after connection.
Failed or uncertain effects retain ownership for reconciliation. The Manager uses
its own role prompt and pinned Type/Skills, without inheriting implementation or
orchestrator project rules. Ordinary session restoration performs no extra Manager
storage reads. Reopened-database tests cover both native modes and exact retained
resources. Production start, inbox and structured tools remain subsequent work.

Stage 14d1 adds the dedicated controller start service on top of that native
boundary. A stable admission ID, exact governance version and reason are the only
start inputs; authority is supplied by the user/daemon boundary. The service
reserves before calling the shared session manager, supplies only the configured
Type/version and governing user's registry authority, and never accepts role or
configuration overrides. Only a newly created reservation can call Spawn. Exact
retries inspect even an unseeded admission or unresolved native generation; they
do not infer that a failed connection permits another process. Current/historical
controller reads expose admission, dispatch and pending-operation facts without
claiming native liveness. API/daemon wiring and classified work delivery follow.

Stage 14d2 binds that service to the daemon's shared session manager and exposes
strict 16 KiB start, current-ownership and exact-controller HTTP/CLI operations.
Start accepts only stable ID, governance version and reason; the controller adds
the human actor. Exact retries remain inspection after policy disable. Responses
retain explicit nulls for absent ownership/binding/pending operations; source-owner
internals never enter the wire shape. Frontend session mapping preserves
`agent_manager` in both summaries and details and excludes Managers from worker
and orchestrator populations. Native work payloads/protocol delivery remain the
next slice; this start operation alone makes no routing or worker-launch decision.

Stage 14e1 seals classified Manager inputs before transport (0174). The store
loads exact task/criteria definitions and governance under the shared native-owner
transaction guard; request reasons and arbitrary caller material never enter this
payload. Clearance comes from the original session's exact Type version, even
when another version is activated. Each immutable, inspectable context records
per-item classes, native generation, exact rendered input, hashes and a link to
the preceding context for this controller. There are at most 32 input versions
per request and 10,000 per controller; no time-based reset bypasses those bounds.

A persistent Manager conversation is itself a compartment. The first scoped
routing input binds its project-local engagement; subsequent differently scoped
requests are refused, even at mission clearance. Unscoped technical material may
flow up. Aggregate sensitivity never decreases across the linked conversation,
and later unscoped input retains an existing engagement binding. Replacement
native generations inherit this boundary. New, confirmed-released controllers
may start another conversation; a probe failure never clears the old boundary.
The retained chain is the binding, avoiding a mutable duplicate of that authority.
SQL guards prevent chain forks, sensitivity downgrade, scope stripping and loss
of retained history. Sealing and audit/CDC are atomic. Exact retries inspect old
bytes after task or policy changes; they authorize no native effect. Transport
acknowledgements and proposal-to-input attribution remain subsequent slices.

Stage 14e2 adds bounded native input delivery (0175). A transaction seals the
classified input and reserves one of at most four native sends. One unfinished
routing request occupies a Manager conversation; ambiguous/dispatching writes
also block later input even if routing intent is independently resolved. Only
proven no-send outcomes permit automatic retry. The fourth refusal atomically
records Needs Human. Stable per-request delivery keys use existing Chat dedup;
TUI retains its lack of recipient acknowledgement. Restart recovery must mark
unresolved claims uncertain, never resend them based on a timeout or probe.

Manager delivery and worker messages share the existing native operation, Chat
relay and guarded TUI writer. Manager transport verifies the actual retained
context/reservation, rechecks live governance/task intent immediately before
writing, and sends the exact sealed bytes. The shared writer preserves explicit
role boundaries. New proposals require a matching native input claim (including
an in-flight claim, because native output can precede transport commit). They
retain both the received input hash and the latest conversation hash/class/scope;
prior sensitive context cannot disappear from a later technical routing result.
Historical unclassified proposal bytes remain readable through optional fields;
new inserts require attributed input. Public delivery/context inspection, daemon
consumption and generation-specific CLI instructions follow in 14e3.

Stage 14e3 wires the daemon inbox consumer after startup native reconciliation
and drains it before closing storage. Startup turns interrupted sends into retained
uncertainty. Each cycle scans at most 16 requests, checkpoints a durable cursor,
and bounds each native attempt to 45 seconds. It can admit the configured Manager
when none is reserved; failed starts retain admission and never authorize another
launch. Unknown readiness consumes no input/send. Superseded task/governance intent
closes explicitly; class/scope refusal becomes Needs Human without embedding data.
Blocked projects do not starve eligible projects on later scan pages.

Sealed inputs now carry project attribution and optional, versioned native CLI
routing (literal executable/argv and AO_RUN_FILE). Protocol v1 includes the exact
request/session/generation, strict proposal-envelope example, read-only registry
inspection and receipt inspection. Its bytes are golden-tested: future instruction
changes require a new renderer version, retaining v1 for historical validation.
Older artifacts without tools retain their hash/bytes. Three scoped API/CLI reads
expose context history, exact input and bounded native delivery attempts, including
classification and unknown outcomes without serializing internal native owners.
Automatic semantic assessment, composition/evolution and creation permissions
remain stage 15; controller recovery and refreshed replacement inputs remain 23.

Stage 14c1 adds the durable Manager inbox (0171). Routing requests reference exact
task/criteria and governance versions plus hashes, without embedding task content.
They are independent of a native controller's lifetime. Enqueue serializes with
planning and policy changes, checks cancellation ancestors and frozen criteria,
allows one pending request per task, and enforces the current per-project queue
limit plus a 10000-request retained-history bound. Exact-ID retries return their
original records after restart, revision changes or terminal resolution.

Only trusted user/orchestrator/system planning context can enqueue or close this
intent; worker/Manager declarations cannot provide that authority. Terminal
cancelled, superseded and Needs Human receipts are immutable and release only
inbox capacity, never leases or native ownership. A validated selection/application
transaction remains separate subsequent work. Requests and receipts share their
business transaction with audit/CDC, expose bounded project-scoped history, and
refuse history-losing downgrade. Manager context delivery must subsequently pass
through the classified Context Builder before native tools receive content.

Stage 14c2 exposes this inbox through the existing Manager service, project-scoped
HTTP routes and thin CLI. Human write bodies are strict 16 KiB JSON with no actor,
session or origin fields. Enqueue requires an explicit stable request ID; exact
retries return the original request/receipt. Pending inbox and full history use
separate bounded list routes. A null resolution explicitly represents pending
intent. These public control reads/writes neither deliver native context nor launch
controllers or workers. Manager native tools use their subsequent generation-fenced
service path. Generated contracts and telemetry route/command allowlists accompany
the six new operations; telemetry identifies routes, not projects or request IDs.

Stage 14c3 retains native proposal receipts (0172) with the exact output, parser
outcome, request hash, native configuration hash and source generation. Strict v1
JSON proposes an exact existing Type or Needs Human with a rationale and bounded
candidate explanations. These explanations remain LLM claims; deterministic
compatibility, ownership, composition and application belong to stage 15. A parsed
selection waits unapplied and cannot be replaced under a new native retry key.

Submission requires the exact current Manager owner, an unreleased project
controller, no unresolved native effects, a confirmed connection receipt for that
generation, current task run intent/revision and unchanged enabled governance.
Exact request/key/output retries return history before these live checks. Empty
and malformed output count against the pinned/current policy's 1–5 correction
budget; exhausted correction or explicit escalation atomically adds a Needs Human
request receipt. This closes routing intent only. All output, audit and escalation
either commit together or roll back. Proposal history survives restart, is bounded
and immutable, and blocks downgrade that would discard it. Native transport/API
and delivery-context sealing remain subsequent integrations, not supplied by this
store boundary.

Stage 14c4 adds the native proposal service/API/CLI and historical reads. The
service derives project and exact owner from the Manager session, checks the
submitting generation, and relies on the writing transaction to recheck all facts.
The outer envelope contains only generation, retry key and raw output; unknown
authority fields are rejected. Its 512 KiB transport budget permits JSON escaping
around at most 64 KiB inner output. A missing/null candidate list is a retained
parser rejection, matching the generated non-null array contract. Historical
proposal reads remain project/request scoped after native termination. These
operations still do not start controllers, deliver task content or apply selection.

Metrics retain denominators and time windows: attempted/completed, first-pass
completion, revisions, CI failures, review findings, retries, duration and usage
when measured. Attribute to all pinned Skills without claiming causality.
Manager routing and orchestrator planning are separate evidence dimensions so
decomposition failures do not automatically count against a worker's quality.

Experiments pin control/candidate versions, comparable task criteria, allocation,
minimum samples, stop conditions and promotion policy before dispatch. Show
missing/confounded evidence and avoid promotion from one successful task.
Recommendations carry an inspectable diff and sample size. Creation, versioning,
selection and promotion policies are enforced separately. Optimization presets
are routing preferences, not an invented calibrated intelligence score.

Dry-run reuses validation, selection and admission simulation with writes and
process launches disabled. It does not create worktrees, auto-install harnesses,
write the target repository or mutate registry/task state. LLM-produced plans
are proposals and clearly identify any model call; estimates report unknown
usage/cost rather than fabricated precision. Execution revalidates the plan
against current state before committing tasks.

## API, events and desktop

Proposed resource groups (final operations must be added to code-first specgen):

- Agent Types/versions, Skills/versions, portable import/export and safe bindings.
- Project adaptive policy/goal/control, tasks/revisions/dependencies and dry-run.
- Attempt configuration/context/results/messages, knowledge and evaluations.
- Manager decisions, metrics, experiments, recommendations and audit queries.

Use strict request validation, stable error envelopes/request IDs, bounded pages,
optimistic concurrency and idempotent mutation keys where retries create work.
Extend the CLI using HTTP only and update the embedded `using-ao` catalog.
Generate OpenAPI/TypeScript from DTOs and operation registrations together.

Audit writes belong in the same transaction as their business mutation. CDC
remains trigger-owned. The current `change_log` event enum needs a new migration
for new event kinds; standalone migration `0140` already permits null project
scope, which allows global registry invalidation without a fake project. Keep
long-lived semantic audit separate from the bounded CDC replay log. Extend SSE
payloads, renderer invalidation and telemetry route templates without leaking
prompts, secrets or raw user identifiers into telemetry.

Add these desktop surfaces using existing routing, React Query, shared product
UI and form primitives:

1. Agent Types and Skills registry: search, origin, versions, active workers,
   capability-gated editors, clone/compare/rollback/disable/import/export/launch.
2. Existing Task Composer: Automatic, pinned Agent Type, or existing/default
   launch. Show one-off overrides without modifying the definition.
3. Session Inspector: task, pinned configuration/Skills, criteria, dependencies,
   context manifest, decision, evidence and links into version history.
4. Project Control Center: goal, manager/orchestrator health, graph summary,
   limits, Needs Human, recommendations, experiments and deterministic controls.
5. Task dependency graph alongside Kanban, with an accessible list/table fallback.
6. Performance comparisons, editable knowledge and chronological audit timeline.
7. Existing agent/auth settings extended with only meaningful local binding
   controls; retain native account/login workflows.

Render loading, empty, stale, failed and disabled states. Preserve keyboard
operation and localization patterns. Manager-created definitions must invalidate
the same registry queries used by manual creation. Shared product UI changes
must remain compatible with the existing Cloud and desktop callers.

## Upstream overlap and design deviations

GitHub metadata was read through the authenticated connector on 2026-09-18.
These PRs were open and unmerged when inspected; their descriptions are not proof
that the local checkout contains the feature:

- [#2848](https://github.com/Untrivial-ai/agent-orchestrator/pull/2848): per-role
  system prompt, environment, MCP and plugin configuration, initially Claude.
- [#3930](https://github.com/Untrivial-ai/agent-orchestrator/pull/3930): user-scope
  config storage without effective launch merge.
- [#5541](https://github.com/Untrivial-ai/agent-orchestrator/pull/5541): per-harness
  model/effort defaults and settings UI.
- [#5256](https://github.com/Untrivial-ai/agent-orchestrator/pull/5256): orchestrator
  rules file and explicit restart to apply changed standing instructions.

Also examined issue reports
[#2195](https://github.com/Untrivial-ai/agent-orchestrator/issues/2195),
[#5367](https://github.com/Untrivial-ai/agent-orchestrator/issues/5367),
[#3720](https://github.com/Untrivial-ai/agent-orchestrator/issues/3720), and
[#3716](https://github.com/Untrivial-ai/agent-orchestrator/issues/3716) concerning
role environments, unavailable skill discovery and shared-data migration hazards.
Do not assume their reported problems were independently reproduced here.

Significant adaptations to the conceptual specification:

- Heterogeneous workers already exist; Agent Types package existing choices.
- A persistent task intent layer is necessary because current task creation is
  immediate spawning. It references, rather than replaces, sessions and Kanban.
- Skills require an authoring/versioning layer, not a replacement native runtime.
- Provider support is mode/adapter-specific. Device-global accounts cannot be
  represented as isolated concurrent per-type accounts.
- Semantic routing runs through existing harness infrastructure; deterministic
  admission and validated service actions remain authoritative.
- Exact adaptive snapshots must extend restore paths, not only initial launch.
- Long-lived audit is distinct from short-lived CDC replay and optional telemetry.

## Verification gates

Each milestone must pass focused tests, relevant full suites, diff/security and
concurrency/restart review before commit. No milestone is complete from code
presence alone. The checklist maps all acceptance requirements to stages.

Use injected clocks, fake registries/controllers, `httptest`, temporary SQLite
databases and the opt-in fake harness. The existing fake harness emits real AO
hooks through a POSIX script; verify shell availability for Windows lab use.
Cover racing reservations, lease fencing, malformed results, stale evidence,
ownership spoofing, approval bypass, path traversal, import secret exclusion,
criteria tampering, dependency cycles, retry exhaustion and each control race.

Run the pinned CI commands, including full backend build/vet/race tests,
golangci-lint v2.12.2, sqlc v1.31.1 and API drift, frontend typecheck/tests,
shared product UI checks, desktop build and applicable e2e gates. Native OS and
remote-check gaps must be reported individually, not labeled passed.

Validate clean and pre-extension databases, restart with active detached
controllers, pinned configurations after default edits, and opt-out behavior.
Finally run the real Electron app in an isolated checkout and scratch
`AO_DATA_DIR`; exercise the full UI and capture exact observed results. No user
production AO database, credentials or runtime artifacts belong in commits.
