# Mission

You are operating inside an existing fresh local clone of the Agent Orchestrator repository:

https://github.com/Untrivial-ai/agent-orchestrator

The repository has already been cloned onto my desktop and your working directory starts inside that repository.

I have NOT created a GitHub fork yet.

Your job is to take this existing AO repository and autonomously implement a major extension: a **user-configurable,
heterogeneous, adaptive multi-agent engineering platform** built natively into Agent Orchestrator.

You have authority to work through the entire implementation autonomously.

Do not stop after creating a plan.

Do not stop after implementing only the backend.

Do not stop after producing a prototype.

Continue through implementation, migrations, API integration, desktop UI, testing, actual application validation,
documentation, commits, and final review until the Definition of Done is satisfied or you encounter a genuine blocker
requiring human action.

---

# IMPORTANT: HOW TO WORK

This is one high-level assignment, but it is NOT a request for one enormous code-generation pass.

Work like a competent engineering team.

Use this loop throughout the project:

```text
inspect
  ↓
understand
  ↓
design
  ↓
implement small coherent unit
  ↓
format/lint
  ↓
test
  ↓
inspect diff
  ↓
fix
  ↓
commit
  ↓
update plan
  ↓
continue
```

Maintain a persistent implementation plan/checklist in the repository.

Commit continuously as verified milestones are completed.

Do not wait until the end to create one enormous commit.

Do not assume this specification perfectly reflects current AO internals.

The current repository source is authoritative.

The capabilities described below are requirements.

The proposed internal implementation is guidance.

If AO already implements something equivalent, REUSE IT.

If AO partially implements it, EXTEND IT.

If the proposed architecture conflicts with AO's existing architecture, redesign the implementation cleanly while
preserving the requested behaviour.

Document significant architectural deviations and why they were necessary.

---

# 0. GIT / FORK SETUP

You are already inside the existing local clone.

First inspect:

```bash
git status
git remote -v
git branch --show-current
git log -1 --oneline
```

Do not destroy or overwrite local work if unexpected changes exist.

The expected situation is a fresh clone.

The original AO repository should become the `upstream` remote.

If authenticated GitHub tooling is available, create a fork of:

`Untrivial-ai/agent-orchestrator`

under my authenticated GitHub account.

Prefer using authenticated GitHub tooling such as `gh` if available.

After creating the fork, configure remotes conceptually as:

```text
origin   = my fork
upstream = Untrivial-ai/agent-orchestrator
```

Verify both.

Create a development branch:

```text
feature/adaptive-agent-platform
```

Push that branch to my fork once appropriate.

If creating the GitHub fork requires authentication or another genuinely human-only action, DO NOT abandon the
engineering work.

Record the blocker clearly, preserve the original repository as `upstream` where possible, create the feature branch
locally, continue implementation and queue the fork/push action as a human-required task.

Record the exact upstream commit SHA from which development began.

---

# 1. CURRENT-STATE AUDIT — MANDATORY BEFORE IMPLEMENTATION

AO is actively developed.

Before building anything substantial, inspect the CURRENT repository thoroughly.

Read relevant files including, where present:

* README
* AGENTS.md
* docs/architecture.md
* docs/backend-code-structure.md
* docs/development.md
* docs/STATUS.md
* project configuration
* domain models
* session manager
* orchestrator
* worker lifecycle
* worker spawning
* agent/harness adapters
* model configuration
* provider configuration
* permissions
* MCP support
* plugins
* skills
* review/evaluator infrastructure
* Git/worktree lifecycle
* persistence
* database migrations
* event system
* daemon/API
* generated frontend client
* Electron application
* React frontend
* Kanban
* project settings
* worker/session UI
* tests

Search the codebase aggressively.

Trace the complete current lifecycle:

```text
User / Orchestrator
        ↓
Task / Worker creation
        ↓
Agent/harness selection
        ↓
Model/provider configuration
        ↓
Session creation
        ↓
Worktree
        ↓
Agent process
        ↓
Git / PR / CI / Review
```

Also inspect current relevant GitHub issues/PRs if network access is available.

Pay particular attention to existing work involving:

* per-role environment configuration;
* orchestrator configuration;
* worker configuration;
* agent/model overrides;
* permissions;
* MCP;
* plugins;
* skills;
* system prompts;
* explicit harness selection;
* worker spawning;
* reviews;
* evaluators;
* browser isolation;
* provider/model controls.

---

# 2. CREATE AN ARCHITECTURE GAP ANALYSIS

Before substantial implementation, create:

`docs/adaptive-agent-platform-design.md`

It must document what AO ALREADY does and what needs to be added.

Include a matrix similar to:

| Capability             | Existing | Partial | Missing | Implementation strategy |
| ---------------------- | -------: | ------: | ------: | ----------------------- |
| Worker lifecycle       |          |         |         |                         |
| Worktree isolation     |          |         |         |                         |
| Harness selection      |          |         |         |                         |
| Model selection        |          |         |         |                         |
| Provider configuration |          |         |         |                         |
| Skills                 |          |         |         |                         |
| Agent Types            |          |         |         |                         |
| Agent Manager          |          |         |         |                         |
| Evaluation             |          |         |         |                         |
| Task dependencies      |          |         |         |                         |
| Project knowledge      |          |         |         |                         |
| Context construction   |          |         |         |                         |
| Audit trail            |          |         |         |                         |
| etc.                   |          |         |         |                         |

Explicitly identify existing AO concepts that should be reused.

DO NOT create parallel implementations of existing AO functionality.

Commit the architecture/gap analysis before beginning major implementation.

---

# 3. TARGET ARCHITECTURE

The conceptual target is:

```text
                           USER
                             │
                    Goal + Policies
                             │
             ┌───────────────┴──────────────┐
             │                              │
             ▼                              ▼
     Manual Configuration             ORCHESTRATOR
             │                      "What needs doing?"
             │                              │
             │                           TASK DAG
             │                              │
             ▼                              ▼
      AGENT TYPE REGISTRY ◄───────── AGENT MANAGER
             │                     "Who/how should do it?"
             │                              │
       ┌─────┴─────┐                  SCHEDULER
       │           │                       │
  Agent Types    Skills             resources/limits
       │           │                       │
       └─────┬─────┘                       │
             ▼                             │
       CONTEXT BUILDER ◄───────────────────┘
             │
      ┌──────┼─────────┐
      ▼      ▼         ▼
   Codex   Claude   OpenCode / other
   Worker   Worker      Worker
      │      │          │
      └──────┼──────────┘
             ▼
       ARTIFACTS/EVENTS
             │
             ▼
         EVALUATOR
             │
       ┌─────┴─────────┐
       ▼               ▼
 ORCHESTRATOR      AGENT MANAGER
project feedback   agent feedback
                       │
                       ▼
                  EXPERIMENTS
                       │
                Agent/Skill evolution
```

AO itself remains the deterministic control plane.

LLMs reason over structured state and invoke validated application capabilities.

LLMs must NOT directly manipulate AO's database.

---

# 4. RESPONSIBILITY SEPARATION

## Orchestrator

The Orchestrator answers:

**What needs to be done?**

Responsibilities:

* understand high-level project goal;
* plan;
* decompose work;
* create tasks;
* establish dependencies;
* establish acceptance criteria;
* determine parallelisable work;
* track project progress;
* respond to completed/failed work;
* create follow-up tasks;
* decide whether the overall goal has been satisfied;
* communicate worker requirements to the Agent Manager.

The Orchestrator should NOT need to know provider-specific CLI invocation details.

---

## Agent Manager

Introduce a first-class persistent Agent Manager.

The Agent Manager answers:

**Who/how should perform this work?**

Responsibilities:

* inspect available Agent Types;
* inspect Skills;
* inspect available harnesses/providers/models;
* understand currently running workers;
* inspect historical worker outcomes;
* select Agent Types;
* compose Agent Types with Skills;
* select appropriate model/provider configuration where supported;
* create specialist Agent Types;
* create Skills;
* create new Agent Type/Skill versions;
* monitor performance;
* identify recurring weaknesses;
* recommend improvements;
* run controlled experiments;
* replace/redirect failed or inappropriate workers;
* respect resource/concurrency policies.

---

## Workers

Workers perform specific tasks.

Preserve AO's existing worker machinery:

* isolated workspace/worktree;
* Git;
* agent process;
* conversation;
* terminal;
* browser;
* commits;
* PR;
* CI;
* review.

Do not unnecessarily replace worker/session lifecycle infrastructure.

---

## Evaluator

The Evaluator answers:

**What objectively happened?**

It provides evidence to both:

```text
Orchestrator → project feedback
Agent Manager → agent-performance feedback
```

---

# 5. AGENT TYPES — FIRST-CLASS USER FEATURE

Implement first-class persisted **Agent Types**.

An Agent Type is a reusable worker configuration.

It is NOT a running worker.

Example:

```text
Rust Specialist

Harness:
Codex

Provider/Auth:
existing configured authentication

Model:
configured model

Instructions:
specialised Rust instructions

Capabilities:
rust
systems-programming
debugging

Skills:
Rust
Security Review

Maximum parallel workers:
3
```

Workers are instances of Agent Types.

---

# 6. TWO EQUAL AGENT-TYPE CREATION PATHS

This is critical.

Agent Types can be created by:

```text
USER
```

through the desktop UI,

OR:

```text
AGENT_MANAGER
```

dynamically.

Both creation paths must use the SAME:

* domain model;
* persistence;
* validation;
* services;
* APIs;
* registry;
* worker-launch system;
* UI.

Do not create separate incompatible systems.

Persist origin such as:

```text
USER
AGENT_MANAGER
SYSTEM
```

---

# 7. USER-CREATED CUSTOM AGENT TYPES

The user must be able to open the AO desktop application and create an Agent Type manually.

Provide a proper UI editor.

Conceptually:

```text
Create Agent Type

Name
Description

Harness
Provider / Authentication Configuration
Model

Capabilities / Tags

Base Instructions / System Prompt

Skills

MCP Configuration
Plugins
Tools
Permissions

Maximum Parallel Workers

Enabled

Agent Manager:
    May select
    May modify
    May create versions
```

Only expose controls that are actually supported by the selected harness.

Do not invent meaningless configuration.

---

# 8. HETEROGENEOUS AGENTS

Different Agent Types must be capable of using different AO-supported harnesses/providers/models.

Example:

```text
Architect
└── Claude Code

Implementation
└── Codex

Researcher
└── OpenCode

Reviewer
└── Claude Code

Test Engineer
└── Codex
```

There must NOT be a single global worker provider forcing every worker to use the same harness.

Existing project worker settings may remain as backwards-compatible defaults.

Agent Types can override them.

---

# 9. CUSTOM PROVIDERS / CONNECTIONS

Where AO/harness architecture supports custom providers, expose them cleanly.

Conceptually:

```text
Harness
   ↓
Provider / Authentication Configuration
   ↓
Model
```

Reuse AO's existing provider/auth abstractions wherever possible.

Agent Types should reference reusable provider/connection configurations.

Do NOT duplicate credentials into every Agent Type.

Do NOT store secrets in plaintext Agent Type records.

Preserve subscription-authenticated CLI operation where the underlying AO adapter already supports it.

Do not unnecessarily convert CLI-authenticated tools into API calls.

---

# 10. AGENT-TYPE OWNERSHIP

Persist who created an Agent Type.

Support policy flags conceptually equivalent to:

```text
manager_can_select
manager_can_modify
manager_can_version
```

Example:

```text
Critical Reviewer

Created by:
USER

Agent Manager may select:
YES

Agent Manager may modify:
NO

Agent Manager may version:
NO
```

The Agent Manager must respect these restrictions.

Never silently rewrite user-owned configurations.

---

# 11. AGENT-TYPE VERSIONING

Agent Types must support versioning.

Example:

```text
rust-specialist-v1
        ↓
rust-specialist-v2
        ↓
rust-specialist-v3
```

Existing workers retain the exact version/configuration used at launch.

Support:

* inspect;
* clone;
* new version;
* compare;
* rollback;
* disable;
* experimental status;
* promotion.

Never silently overwrite known-good versions.

---

# 12. MANUAL WORKER CREATION

Users must be able to manually launch a worker from an Agent Type.

Example:

```text
Agent Type:
Rust Specialist

Task:
Implement X

Context:
...

[Launch Worker]
```

The resulting worker must retain normal AO behaviour.

---

# 13. PER-WORKER OVERRIDES

Where sensible, support one-off overrides:

```text
Agent Type
Model override
Additional instructions
Temporary Skills
```

Do NOT mutate the Agent Type.

Persist the worker's effective configuration for reproducibility.

---

# 14. SKILLS — SEPARATE FROM AGENT TYPES

Do not create a completely new Agent Type for every combination of knowledge.

Implement or extend AO's existing Skills functionality into a first-class composable registry.

Conceptually:

```text
Agent Type
   │
   ├── Harness
   ├── Model
   ├── Base Instructions
   │
   └── Skills
        ├── Rust
        ├── PostgreSQL
        ├── Security Review
        └── Repository Knowledge
```

Skills may contain appropriate:

* instructions;
* knowledge;
* references/resources;
* capability metadata;
* tool requirements;
* MCP requirements.

Skills must support:

* user creation;
* Agent Manager creation;
* editing;
* versioning;
* enabling/disabling;
* attachment to Agent Types;
* temporary attachment to tasks/workers;
* import/export where appropriate.

Reuse current AO skill mechanisms wherever possible.

Do not create a competing skill system if AO already has suitable primitives.

---

# 15. AGENT MANAGER CREATION STRATEGY

When the Agent Manager receives a requirement, it should reason through approximately:

```text
1. Can an existing Agent Type do this?

2. Can an existing Agent Type + existing Skills do this?

3. Should an existing Agent Type receive a new version?

4. Is a new Skill required?

5. Is an entirely new Agent Type required?
```

Avoid uncontrolled profile explosion.

---

# 16. DYNAMIC AGENT CREATION

The Agent Manager must be able to determine that existing agents are insufficient.

Example requirement:

```text
Analyse an undocumented binary format and implement a parser.
```

Existing types:

```text
General Coder
Frontend
Researcher
Reviewer
```

Manager may create:

```text
Binary Format Specialist v1
```

The new Agent Type must:

* use normal persisted Agent Type entities;
* appear immediately in the UI;
* be inspectable;
* have origin `AGENT_MANAGER`;
* be manually usable by the user;
* have version history;
* have audit history;
* participate in evaluation/performance tracking.

---

# 17. AGENT TYPE / SKILL REGISTRY

Create a unified management experience for:

```text
Agent Types
Skills
Versions
Capabilities
Origin
Performance
Active workers
```

Support filtering/search.

---

# 18. IMPORT / EXPORT

Allow Agent Types and Skills to be imported/exported.

Never export secrets.

Portable definitions may include:

```text
name
description
harness identifier
model preference
instructions
capabilities
Skills
tool configuration
policy configuration
```

Authentication/provider bindings should be re-established locally where necessary.

---

# 19. PERSISTENT PROJECT KNOWLEDGE

Implement a structured project knowledge layer.

Workers should not repeatedly rediscover important facts.

Store durable information such as:

```text
architectural decisions
repository conventions
important interfaces
constraints
known pitfalls
failed approaches
important file relationships
external behaviour discovered
unresolved questions
```

Workers may propose knowledge.

Do not automatically treat every worker statement as canonical truth.

Persist:

* content;
* source;
* provenance;
* timestamp;
* confidence/status where useful;
* superseded/invalidated state.

The user must be able to inspect and edit this knowledge.

---

# 20. CONTEXT BUILDER

Implement a first-class Context Builder.

Do NOT dump the entire project history into every worker.

Construct task-specific context from:

```text
task
acceptance criteria
parent task
dependencies
relevant repository files
project knowledge
previous findings
interface contracts
relevant Skills
Agent Type instructions
```

Record enough provenance to understand what context a worker received.

---

# 21. STRUCTURED WORKER RESULTS

Workers should produce structured results in addition to Git changes.

Conceptually:

```text
Task Result

summary
implementation
decisions
assumptions
interfaces changed
tests performed
findings
unresolved issues
recommended follow-up
knowledge candidates
```

Integrate this into existing AO session/task mechanisms where possible.

---

# 22. WORKER-TO-WORKER COMMUNICATION

Support controlled worker communication.

Prefer structured messages/artifacts over unrestricted cross-agent chat.

Useful message types:

```text
finding
question
answer
blocker
handoff
interface_contract
review_request
dependency_update
```

The Orchestrator should be able to observe these communications.

Persist them where useful.

---

# 23. PERSISTENT TASK DAG

Implement or extend AO's task model into a persistent dependency graph.

Support states conceptually including:

```text
planned
blocked
ready
leased
working
review
failed
completed
needs-human
cancelled
```

Support:

* dependencies;
* priorities;
* parallel work;
* retries;
* follow-up tasks;
* cancellation;
* ownership;
* blocking relationships.

Reuse existing AO task/session/Kanban concepts where possible.

Do not create duplicate competing task systems unnecessarily.

---

# 24. FROZEN ACCEPTANCE CRITERIA

Before implementation begins, persist acceptance criteria.

Example:

```text
AUTH-17

Requirements:
- OAuth callback implemented
- invalid state rejected
- refresh supported
- tests added

Verification:
- authentication tests pass
- build succeeds
- security review has no blocking findings
```

Do not allow the implementing worker to redefine success after seeing its own result.

Criteria may later be revised by the user/Orchestrator, but revisions must be versioned/audited.

---

# 25. TASK LEASES / OWNERSHIP

If AO does not already provide equivalent reliable semantics, implement task leases.

Track:

```text
task
worker
lease state
heartbeat
last activity
```

If a worker disappears, AO must identify abandoned work and safely reassign/recover it.

Prevent duplicate workers unknowingly performing the same exclusive task.

---

# 26. SCHEDULER

The Agent Manager may decide WHO should do work.

AO's deterministic scheduler decides WHETHER/WHEN that worker can be launched.

Consider:

```text
task dependencies
priority
global worker limit
per-Agent-Type limit
currently running workers
harness availability
provider availability
machine resources
usage/rate information where observable
retry limits
```

Do not allow an LLM to directly create unlimited processes.

---

# 27. CAPABILITY DISCOVERY

Maintain machine-readable information about what available harnesses/configurations actually support.

Examples:

```text
model selection
provider selection
MCP
browser
shell
image input
resume
subagents
permissions
context features
```

The Agent Manager should not assign tasks requiring unavailable capabilities.

Reuse adapter metadata if AO already exposes this.

---

# 28. EVALUATOR

Implement a proper evaluation subsystem.

Capture deterministic evidence wherever possible:

```text
task completion
build result
tests
CI
lint
review findings
mergeability
merge conflicts
worker crash
retries
orchestrator rejection
human intervention
duration
usage information where exposed
```

LLM qualitative review may supplement this.

It must not replace objective evidence.

Avoid:

```text
worker says work is excellent
        ↓
system records excellent
```

Prefer:

```text
tests
build
CI
review
acceptance criteria
        ↓
evaluation
```

---

# 29. INDEPENDENT REVIEW

Support policies such as:

```text
implementation Agent Type != reviewer Agent Type
```

For important tasks, allow workflows such as:

```text
Codex implementation
       ↓
Claude review
       ↓
separate test worker
```

Reuse AO's current review system wherever possible.

---

# 30. PERFORMANCE HISTORY

Maintain evidence per:

```text
Agent Type
Agent Type Version
Skill
Skill Version
Harness
Model
Task category/capability
```

Useful aggregates:

```text
attempted
completed
first-pass completion
revision count
CI failures
review findings
retry count
duration
```

Always preserve sample sizes.

Do NOT create a fake universal "agent intelligence score."

---

# 31. EVALUATE THE AGENT MANAGER

The Agent Manager itself must be observable.

Persist decisions:

```text
task
requirements
candidate Agent Types
selected Agent Type
selected Skills
reason
new profile created?
new Skill created?
outcome
replacement required?
```

This enables analysis of whether routing decisions actually improve.

---

# 32. EVALUATE THE ORCHESTRATOR

Track orchestration quality indicators such as:

```text
tasks created
tasks abandoned
tasks repeatedly rewritten
late-discovered dependencies
unnecessary workers
dependency mistakes
human corrections
```

Do not automatically blame workers for failures caused by poor decomposition.

---

# 33. CONTROLLED EXPERIMENTS

Introduce explicit experiments for Agent Type/Skill evolution.

Example:

```text
Experiment #17

Hypothesis:
Rust-v4 reduces review revisions.

Control:
Rust-v3

Candidate:
Rust-v4

Comparable tasks:
12
```

Do not promote configurations because of one successful task.

---

# 34. OPTIMIZATION POLICY

Allow users to configure what the Agent Manager optimizes for.

Presets may include:

```text
Maximum Quality
Balanced
Fast Iteration
Minimise Usage
```

Advanced configuration may allow relative priorities such as:

```text
Quality: 70
Speed: 20
Usage/Cost: 10
```

Treat these as policy preferences, not scientifically precise scores.

---

# 35. CONTROLLED AGENT EVOLUTION

The Agent Manager should inspect historical evidence.

Example:

```text
Frontend-v2

11 tasks
6 revisions

Recurring issues:
- accessibility
- missing tests
```

If permitted, it may create:

```text
Frontend-v3
```

with improved configuration.

Never silently mutate v2.

If user policy prohibits automatic versioning, create a recommendation instead.

---

# 36. RECOMMENDATIONS

Support recommendations as persisted objects.

Example UI:

```text
Agent Manager Recommendation

Frontend Specialist v2

Observation:
6/11 recent tasks required revision.

Common issues:
- accessibility
- component tests

Recommendation:
Create v3 with stronger requirements.

[Dismiss]
[Inspect Changes]
[Create Version]
```

---

# 37. AGENT MANAGER TOOLS

Expose validated application-level capabilities equivalent to:

```text
list_agent_types
get_agent_type
get_agent_type_versions

create_agent_type
create_agent_type_version
compare_agent_types
disable_agent_type

list_skills
get_skill
create_skill
create_skill_version

list_available_harnesses
list_available_models
list_provider_configs

list_active_workers
get_worker_state
get_worker_evaluation

get_agent_type_metrics
get_skill_metrics

delegate_task
spawn_worker
redirect_worker
stop_worker

create_recommendation
create_experiment
```

Use existing AO services internally.

Do not duplicate worker/session lifecycle logic.

---

# 38. ORCHESTRATOR ↔ AGENT MANAGER PROTOCOL

Communication should be structured.

Conceptually:

```text
Orchestrator:

delegate_task({
    task,
    requirements,
    capabilities,
    context,
    dependencies,
    acceptance_criteria,
    priority
})
```

Agent Manager responds with structured selection information.

Persist useful decision metadata.

The Orchestrator should not need to manually specify CLI commands.

---

# 39. AUTONOMOUS LOOP

Support:

```text
Goal
 ↓
Orchestrator plans
 ↓
Task DAG created
 ↓
Acceptance criteria created
 ↓
Ready work identified
 ↓
Agent Manager evaluates requirements
 ↓
Agent Type + Skills selected
          OR
new Skill/Profile created
 ↓
Scheduler authorizes worker
 ↓
Worker executes
 ↓
Artifacts/events captured
 ↓
Evaluator evaluates
 ↓
Orchestrator receives project feedback
 ↓
Agent Manager receives agent feedback
 ↓
Knowledge updated where appropriate
 ↓
Follow-up work identified
 ↓
repeat
```

Critical state must survive daemon/application restarts.

---

# 40. NEEDS HUMAN

Support explicit:

```text
Needs Human
```

states.

Examples:

* missing credentials;
* account creation;
* spending;
* external approval;
* ambiguous product decisions;
* destructive operations;
* security-sensitive permission changes.

Do not block unrelated DAG branches.

Only dependent work should become blocked.

---

# 41. GLOBAL AUTONOMOUS CONTROLS

Implement deterministic project controls:

```text
Pause Project
Resume Project
Drain Workers
Stop After Current Tasks
Cancel Pending Work
Cancel All
```

These must actually affect AO scheduling/process behaviour.

Do not implement them as prompts asking an LLM to stop.

---

# 42. DRY-RUN / SIMULATION MODE

Add a mode that can produce:

```text
Goal
 ↓
proposed plan
 ↓
proposed Task DAG
 ↓
proposed Agent Type/Skill selections
 ↓
proposed parallel workers
 ↓
resource/usage estimate where possible
```

without launching workers or modifying the target repository.

Allow inspection before execution.

---

# 43. UI — AGENT TYPES

Add a first-class Agent Types view.

Example:

```text
Agent Types

+ Create Agent Type

Architect
Claude Code • <model>
User Created
v3
1 active worker

Backend
Codex • <model>
User Created
v4
3 active workers

Binary Specialist
Codex • <model>
Agent Manager Created
v1
0 active workers
```

Support:

* create;
* edit;
* clone;
* version;
* compare;
* rollback;
* disable;
* inspect;
* export;
* import;
* launch worker;
* inspect performance.

---

# 44. UI — SKILLS

Add Skills management.

Show:

```text
Skill
Version
Origin
Capabilities
Attached Agent Types
Usage
Performance evidence
```

Support both user-created and Agent-Manager-created Skills.

---

# 45. UI — PROVIDERS / CONNECTIONS

If existing AO UI does not adequately expose reusable provider/auth configurations, extend it.

Conceptually:

```text
Providers / Connections

Codex CLI
Authenticated ✓

Claude Code
Authenticated ✓

OpenCode
  ├── Provider A
  ├── Provider B
  └── Custom Provider

+ Add Provider Configuration
```

Use secure credential mechanisms.

Agent Types reference configurations rather than copying secrets.

---

# 46. UI — AGENT MANAGER

Create an Agent Manager dashboard.

Show:

```text
Status
Harness / Model

Current worker population

Recent selections

Agent Types created

Skills created

Recommendations

Experiments

Performance observations

Recent failures
```

Allow inspection of why the manager:

* selected an agent;
* rejected alternatives;
* created a type;
* created a Skill;
* created a new version;
* recommended a change.

---

# 47. UI — MANUAL TASK CREATION

Allow:

```text
Agent Selection

○ Automatic — Agent Manager chooses

○ Select Agent Type
      [ Backend Coder ▼ ]

○ Existing/default AO behaviour
```

Users must never be forced to use automatic selection.

---

# 48. UI — TASK GRAPH

Add a useful task/dependency visualization alongside the existing Kanban.

Do not unnecessarily replace useful existing AO UI.

Display:

```text
planned
blocked
ready
working
review
failed
completed
needs-human
cancelled
```

and dependencies.

---

# 49. UI — WORKER DETAIL

Worker/session UI should expose:

```text
Task

Agent Type
Agent Type Version

Skills
Skill Versions

Harness
Provider
Model

Created by

Parent Task
Dependencies

Acceptance Criteria

Context provenance

Current state

Evaluation

Agent Manager decision
```

Allow navigation:

```text
Worker
 ↓
Agent Type
 ↓
Version History
 ↓
Performance
```

---

# 50. UI — PERFORMANCE

Provide Agent Type/Skill/version performance views.

Show useful evidence:

```text
tasks attempted
tasks completed
first-pass completion
revisions
CI failures
review findings
duration
sample size
recent failures
```

Support comparison.

Do not overstate weak evidence.

---

# 51. UI — PROJECT KNOWLEDGE

Allow users to inspect persistent project knowledge.

Support:

```text
search
inspect provenance
edit
pin
invalidate
delete
```

Do not turn project memory into an opaque LLM blob.

---

# 52. UI — AUDIT TRAIL

Create a unified autonomous activity timeline.

Example:

```text
14:02 Orchestrator created AUTH-17

14:03 Agent Manager evaluated 5 Agent Types

14:03 Backend-v4 selected

14:03 Worker #381 spawned using Codex

14:19 PR #123 opened

14:22 CI failed

14:23 Evaluator recorded test failure

14:24 remediation requested

14:37 CI passed

14:39 Reviewer identified two issues

14:40 AUTH-18 created
```

Include human actions:

```text
15:02 User created Rust Specialist v1

15:04 User disabled manager modification

15:08 User manually launched Worker #402
```

---

# 53. UI — AUTONOMOUS CONTROL CENTER

Provide a project-level control surface showing:

```text
Goal

Orchestrator state

Agent Manager state

Task DAG summary

Workers running

Needs Human

Recommendations

Experiments

Resource/usage information

Recent autonomous decisions

Pause
Resume
Drain
Stop
```

---

# 54. RESOURCE / OPERATIONAL LIMITS

Expose configurable controls.

At minimum consider:

```text
maximum simultaneous workers

maximum workers per Agent Type

maximum retries per task

maximum dynamically-created Agent Types

maximum experimental profiles

maximum task depth

maximum pending tasks

maximum concurrent experiments
```

Approval policies may include:

```text
require approval before Agent Type creation

require approval before Skill creation

require approval before user-profile versioning

require approval before profile promotion

require approval before merge

require approval for destructive operations
```

Prevent runaway recursive worker/profile creation.

---

# 55. REPRODUCIBILITY / PROVENANCE

Every worker should be reproducible as far as practical.

Persist something conceptually like:

```text
Worker #821

Agent Type:
Backend-v4

Agent Type version/hash:
...

Skills:
Rust-v2
Security-v3

Harness:
Codex

Provider:
...

Model:
...

Task version:
7

Acceptance Criteria version:
3

Context snapshot/reference:
...

Agent Manager decision:
#991

One-off overrides:
...
```

Autonomous behaviour must be inspectable.

---

# 56. DATABASE / PERSISTENCE

Use AO's existing persistence and migration system.

Do not destructively modify old migrations.

Likely concepts include:

```text
AgentType
AgentTypeVersion

Skill
SkillVersion
AgentTypeSkill

Task
TaskDependency
TaskLease

AcceptanceCriteriaVersion

Evaluation

AgentObservation
AgentMetric

AgentManagerDecision

Experiment

Recommendation

ProjectKnowledge

WorkerArtifact

AgentMessage

AuditEvent
```

These names are suggestions.

First inspect existing AO entities and reuse/extend them.

Do NOT duplicate existing worker/session/review entities unnecessarily.

---

# 57. API ARCHITECTURE

Follow AO's existing boundaries.

Conceptually:

```text
React/Electron UI
       ↓
daemon API
       ↓
application/domain services
       ↓
ports
       ↓
adapters/persistence/runtime
```

Do not put core business logic in React.

Do not allow frontend components to manipulate persistence directly.

Do not allow LLM agents to directly mutate the database.

Regenerate API clients/types using AO's normal mechanism where required.

---

# 58. FAILURE HANDLING

Explicitly design/test:

```text
worker crash
agent CLI crash
authentication expiry
daemon restart
frontend restart
Git conflict
build failure
test failure
unavailable harness
unavailable model
provider failure
deleted provider configuration
invalid Agent Type
invalid Skill
Agent Manager failure
Orchestrator failure
Evaluator failure
duplicate task
expired lease
retry exhaustion
malformed worker result
partial worker completion
```

Persist enough state for recovery.

---

# 59. BACKWARDS COMPATIBILITY

Existing AO projects must continue working.

Adaptive Agent Management should initially be opt-in unless current architecture strongly suggests otherwise.

Without it:

```text
Orchestrator
     ↓
existing AO worker behaviour
```

must remain valid.

Do not break existing project configuration.

Provide migrations/defaults appropriately.

---

# 60. UPSTREAM COMPATIBILITY

AO is actively developed.

Keep this implementation modular.

Avoid unnecessary invasive modifications.

Periodically:

```bash
git fetch upstream
```

and assess upstream changes.

Where practical during the implementation:

```text
update from upstream
resolve conflicts
run tests
continue
```

Document unavoidable divergence.

Do not casually overwrite upstream changes.

---

# 61. TESTING

Add substantial automated testing.

Normal tests must NOT require paid model calls.

Use fake/mock harnesses for integration tests.

Cover at minimum:

```text
Agent Type CRUD
Agent Type versioning
Agent Type ownership
manager permissions

Skills
Skill versioning
Skill composition

provider selection
harness selection
model selection

manual worker spawning
automatic worker selection
per-worker overrides

dynamic Agent Type creation
dynamic Skill creation

Context Builder
project knowledge

Task DAG
dependencies
acceptance criteria
leases

structured worker results
worker messaging

evaluation
performance aggregation

Agent Manager decisions
Agent Manager evaluation
Orchestrator evaluation

experiments
recommendations

scheduler limits
retry limits
runaway-spawn prevention

Needs Human

pause
resume
drain
cancel

dry-run

audit events

daemon restart/recovery

database migrations

API behaviour

frontend state

backwards compatibility
```

---

# 62. ACTUALLY RUN THE APPLICATION

Do not consider compilation sufficient.

During development, actually launch AO.

Exercise the desktop UI.

Where graphical/browser/computer-use capabilities are available, inspect the UI directly.

Verify:

* layouts;
* forms;
* navigation;
* loading states;
* empty states;
* errors;
* disabled controls;
* persistence after restart;
* Agent Type creation;
* Skill creation;
* manual worker selection;
* Agent Manager configuration;
* task graph;
* performance views;
* audit trail;
* autonomous controls.

Fix visual/functional issues you discover.

---

# 63. DEVELOPMENT SEQUENCE

Do not attempt everything simultaneously.

Recommended sequence:

```text
1. Git/fork/remotes setup
2. repository audit
3. architecture gap analysis
4. architecture/design document
5. schema/domain foundations

6. Agent Types
7. Agent Type API
8. Agent Type UI

9. Skills
10. Skills API
11. Skills UI

12. provider/harness/model integration
13. worker ↔ Agent Type integration
14. manual worker launch

15. Task DAG
16. acceptance criteria
17. task leases

18. project knowledge
19. Context Builder
20. structured worker artifacts
21. worker communication

22. Evaluator
23. performance history

24. Agent Manager domain/service
25. Agent Manager tools
26. automatic Agent Type selection
27. dynamic Skill creation
28. dynamic Agent Type creation

29. deterministic scheduler
30. Orchestrator ↔ Agent Manager integration
31. autonomous loop

32. Agent Manager evaluation
33. Orchestrator evaluation
34. experiments
35. profile/Skill evolution
36. recommendations

37. dry-run
38. global autonomous controls
39. Needs Human integration

40. Task Graph UI
41. Agent Manager UI
42. Performance UI
43. Project Knowledge UI
44. Audit UI
45. Autonomous Control Center

46. recovery/failure handling
47. integration tests
48. real application validation
49. upstream compatibility pass
50. documentation
51. final architectural review
52. final cleanup/refactoring
```

Modify this sequence if the actual repository architecture provides a better dependency order.

---

# 64. GIT DISCIPLINE

Commit continuously.

Example commits:

```text
docs: audit current AO orchestration capabilities

docs: design adaptive agent platform

feat(agent-types): add versioned agent type domain

feat(ui): add agent type management

feat(skills): add composable skill registry

feat(ui): add skill management

feat(workers): launch workers from agent types

feat(tasks): add persistent task dependency graph

feat(tasks): add versioned acceptance criteria

feat(knowledge): add persistent project knowledge

feat(context): add task-specific context builder

feat(evaluation): capture worker outcome evidence

feat(agent-manager): add management service

feat(agent-manager): add adaptive worker selection

feat(agent-manager): support dynamic skills

feat(agent-manager): support dynamic agent types

feat(scheduler): add resource-aware scheduling

feat(experiments): add agent configuration experiments

feat(ui): add agent manager dashboard

feat(ui): add task dependency graph

feat(ui): add autonomous control center

test: add adaptive platform integration coverage

docs: document adaptive multi-agent platform
```

Before each meaningful commit:

```text
format
lint
run relevant tests
inspect git diff
remove accidental/generated junk where inappropriate
verify no secrets
```

Push the feature branch periodically once the fork is available.

Never commit:

* passwords;
* API keys;
* OAuth tokens;
* CLI session credentials;
* `.env` secrets;
* provider secrets;
* unrelated personal files.

---

# 65. MAINTAIN A LIVE IMPLEMENTATION CHECKLIST

Create a persistent implementation checklist in the repository.

Update it as work progresses.

For every major requirement mark:

```text
NOT STARTED
IN PROGRESS
IMPLEMENTED
TESTED
VALIDATED IN RUNNING APP
BLOCKED
```

Do not mark a feature complete merely because code exists.

---

# 66. SELF-REVIEW AFTER EACH MAJOR MILESTONE

After each major subsystem:

1. inspect the complete diff;
2. look for architectural duplication;
3. look for missing error handling;
4. look for race/concurrency issues;
5. look for persistence/restart issues;
6. look for security problems;
7. look for untested behaviour;
8. run relevant tests;
9. fix findings;
10. commit.

Do not knowingly carry major broken architecture forward because later phases depend upon it.

---

# 67. CRITICAL ENGINEERING PRINCIPLES

## AO owns state.

LLMs reason over state.

Do NOT implement the system as giant prompts that "remember" everything.

---

## Deterministic code owns deterministic behaviour.

Use normal code for:

```text
permissions
state transitions
leases
limits
scheduling constraints
process management
persistence
Git operations
auditing
recovery
```

Use LLMs where semantic reasoning is useful.

---

## Do not trust self-evaluation.

Prefer:

```text
tests
build
CI
review
acceptance criteria
runtime evidence
```

over an agent's opinion of its own work.

---

## Avoid Agent Type explosion.

Prefer:

```text
existing Agent Type
        ↓
existing Agent Type + Skills
        ↓
new Agent Type version
        ↓
new Skill
        ↓
entirely new Agent Type
```

where appropriate.

---

## Preserve provenance.

Every important autonomous action should be explainable later.

---

## Preserve existing AO strengths.

Do not replace good existing functionality merely to make the new architecture conceptually cleaner.

---

# 68. HUMAN BLOCKER POLICY

Do not stop because something minor requires human action.

Create/record a human-required item and continue independent work wherever possible.

Only stop the entire project if the blocker genuinely prevents meaningful further implementation.

Examples of potentially human-required actions:

```text
GitHub authentication
account creation
credentials
subscription login
external spending
security-sensitive approval
```

If GitHub fork creation is blocked by authentication, continue locally.

If Claude/Codex/OpenCode authentication is unavailable, test through mocks/fakes and clearly record which live
integration remains unvalidated.

---

# 69. DEFINITION OF DONE

Do NOT declare completion merely because a lot of code has been written.

The project is complete when I can open AO and:

1. create Agent Types manually through the UI;

2. configure different AO-supported harnesses/providers/models per Agent Type;

3. configure custom provider options where supported;

4. create Skills manually;

5. attach multiple Skills to Agent Types;

6. configure Agent Manager permissions for user-created Agent Types;

7. manually launch a worker from an Agent Type;

8. create a task and explicitly select an Agent Type;

9. alternatively select Automatic and allow the Agent Manager to choose;

10. give the Orchestrator a high-level project goal;

11. have the Orchestrator create persistent tasks/dependencies;

12. have acceptance criteria established before implementation;

13. have the Agent Manager inspect available Agent Types and Skills;

14. have it select appropriate existing Agent Types;

15. have it compose Agent Types with Skills;

16. have it dynamically create a Skill when appropriate;

17. have it dynamically create an Agent Type when appropriate;

18. see manager-created Agent Types/Skills immediately in the normal UI;

19. run heterogeneous workers concurrently;

20. retain normal AO worktree/session/Git/PR/CI/review behaviour;

21. allow structured worker communication/handoffs;

22. persist useful project knowledge;

23. construct task-specific worker context;

24. independently evaluate worker outcomes;

25. track Agent Type/Skill performance;

26. track Agent Manager routing outcomes;

27. track Orchestrator planning outcomes;

28. conduct controlled Agent Type/Skill experiments;

29. recommend or create improved versions subject to policy;

30. inspect Agent Type versions/performance;

31. inspect task dependencies visually;

32. inspect Agent Manager decisions;

33. inspect persistent project knowledge;

34. inspect a chronological audit trail;

35. see Needs Human items;

36. continue unrelated DAG branches when another branch Needs Human;

37. dry-run an autonomous plan without launching workers;

38. pause the project;

39. resume the project;

40. drain workers;

41. cancel pending/all work;

42. enforce worker/profile/retry limits;

43. recover correctly after AO restart;

44. retain existing AO functionality when adaptive management is disabled;

45. run the relevant automated tests successfully;

46. build the backend successfully;

47. build the desktop application successfully;

48. actually exercise the major new functionality in the running application.

---

# 70. FINAL VALIDATION

When implementation appears complete, perform a dedicated validation phase.

Do NOT immediately report completion.

First:

1. fetch current `upstream`;
2. assess/merge/rebase appropriate upstream changes;
3. resolve conflicts carefully;
4. run formatting;
5. run lint/static checks;
6. run complete relevant backend tests;
7. run frontend tests;
8. build backend;
9. build desktop application;
10. test clean database;
11. test migration from an existing AO database;
12. test daemon restart;
13. test active-task recovery;
14. test manually-created Agent Types;
15. test manager-created Agent Types;
16. test heterogeneous harness configurations where locally available;
17. test Skills;
18. test dynamic Skills;
19. test ownership restrictions;
20. test Task DAG;
21. test acceptance criteria;
22. test task leases;
23. test Context Builder;
24. test project knowledge;
25. test worker artifacts/messages;
26. test Evaluator;
27. test Agent Manager decisions;
28. test experiments;
29. test concurrency limits;
30. test retry limits;
31. deliberately test runaway-spawn prevention;
32. test Needs Human;
33. test dry-run;
34. test pause/resume/drain;
35. inspect the actual desktop UI;
36. inspect the complete diff against upstream;
37. look for redundant implementations of AO functionality;
38. remove debug/temporary code;
39. update documentation;
40. update the implementation checklist;
41. commit remaining validated changes;
42. push the feature branch if GitHub fork access is available.

---

# 71. FINAL REPORT

Only after validation, provide me with:

```text
Fork URL
Feature branch
Starting upstream SHA
Final upstream SHA incorporated

Commit history

Architecture summary

Original AO functionality reused

Original AO functionality extended

New domain entities

Database migrations

New/changed APIs

New UI surfaces

Agent Manager capabilities

Agent Type capabilities

Skill capabilities

Task DAG architecture

Evaluation architecture

Context/knowledge architecture

Tests added

Exact test results

Build results

Live UI validation performed

Harness/provider combinations actually tested

Features implemented but not live-tested

Known limitations

Human-required actions

Upstream divergence

Technical debt

Recommended next improvements
```

Be precise.

Do not claim something was tested when it was merely implemented.

Do not claim something works with a provider/harness that was not actually available for validation.

---

# FINAL INSTRUCTION

Do not repeatedly ask me what to do next.

Do not stop at arbitrary milestones merely because the task is large.

Use the repository, tests, running application, Git history, architecture documentation and actual observed behaviour
as your source of truth.

When uncertainty exists:

```text
inspect
test
measure
then decide
```

rather than guessing.

Continue autonomously until the Definition of Done has been validated or a genuine human-only blocker prevents
meaningful further progress.

