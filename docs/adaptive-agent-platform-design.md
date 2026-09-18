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

Worker results use a versioned schema containing summary, implementation,
decisions, assumptions, interfaces, tests, findings, unresolved issues,
follow-ups and knowledge candidates. Reject malformed/oversized submissions
without losing partial worker output. Self-reported tests are claims, not
independently verified evidence.

Typed messages cover finding, question, answer, blocker, handoff,
interface_contract, review_request and dependency_update. Validate sender/target
project and attempt ownership; persist first, then deliver via existing
mode-aware `Send`/Chat delivery. Correlation IDs and delivery keys prevent
duplicate effects after restart. The orchestrator observes the same timeline.

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

Manager selection first filters permitted, enabled, compatible types, then
considers existing Skills before proposing evolution or new types. Persist
candidates, rejection reasons, selected versions, policy preference, and rationale.
No deterministic tag matcher is presented as semantic LLM planning. Provide a
planner/selector boundary with scripted test doubles and a production controller
using AO's configured native harness. Malformed proposals remain unapplied with
bounded retry or Needs Human; all accepted proposals pass the same services.

Persist manager policy/status, its controller/conversation binding, work inbox and
decisions. Reuse the conversation/runtime execution engine. Decide any required
session-kind extension explicitly with migrations and lifecycle tests; do not
hide a manager among ordinary implementation workers or reimplement processes.
The orchestrator continues to own goal decomposition and calls structured task
and delegation capabilities. Its instructions reference those capabilities, not
provider-specific CLI invocation strings.

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
