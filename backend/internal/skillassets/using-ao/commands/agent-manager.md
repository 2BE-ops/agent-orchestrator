# Agent Manager governance and routing inbox

`ao agent-manager` uses the daemon API and returns JSON.

```bash
ao agent-manager show <project>
ao agent-manager configurations <project> --limit 20
ao agent-manager configuration <project> <version>
ao agent-manager audit <project> --cursor 0 --limit 20
ao agent-manager configure <project> --file manager.json
ao agent-manager start <project> --file start.json
ao agent-manager current <project>
ao agent-manager controller <project> <controller-id>
ao agent-manager inbox <project> --cursor 0 --limit 20
ao agent-manager requests <project> --cursor 0 --limit 20
ao agent-manager request <project> <request>
ao agent-manager request-resolution <project> <request>
ao agent-manager enqueue <project> --file routing.json
ao agent-manager resolve <project> <request> --file resolution.json
ao agent-manager propose <session-id> <request> --file proposal-envelope.json
ao agent-manager proposals <project> <request>
ao agent-manager proposal <project> <request> <proposal>
ao agent-manager contexts <project> <request>
ao agent-manager context <project> <request> <context>
ao agent-manager deliveries <project> <request>
ao agent-manager candidates <project> <request> --limit 20
ao agent-manager candidate <project> <request> <type> --version 1
ao agent-manager decisions <project> <request>
ao agent-manager decision <project> <request> <proposal>
```

`configure` is a human governance action. Manager tools cannot grant themselves
policy permissions. Its API JSON includes `definition`, `expectedRevision` and
`reason`; origin/actor fields are not accepted. Use revision 0 only for initial
configuration, then the current configuration's `number`. A stale edit conflicts.
Configuration edits do not launch a controller or alter running configuration.

`candidates` checks a page of active exact Type versions against the request's
pinned task requirements. It includes exclusions, such as disabled/protected Types
or Skills, insufficient clearance, missing explicit capabilities and unavailable
native configuration. Limits are 1-20; follow `nextCursor` with `--cursor` until
absent before concluding the registry has no suitable candidate. `candidate`
checks a specific historical version; a higher-clearance active version cannot
raise that version's clearance. Project defaults and native provider/capability
checks use the same registry service as worker launch. These are current
observations, not persisted decisions or permission to launch. Native protocol v2
provides both commands; sealed protocol-v1 history retains its original bytes.

`start` is an explicit user-directed native launch under that governance. The
16 KiB JSON body contains only `id`, `configurationVersion` (1-1000), and `reason`:

```json
{"id":"manager-start-1","configurationVersion":1,"reason":"Start the configured project Manager"}
```

Retain that stable ID for retries. Only a new admission may call the native
engine; an exact retry inspects retained state even after a failed connection or
an unseeded crash. Changed fields conflict. `current` returns `state: null` when
there is no reserved controller. `controller` inspects an exact admission after
policy changes or termination. The response includes the native session binding
and any unresolved operation; absence of an operation is not a liveness claim.
Do not create another start ID to work around uncertain native ownership. The
configured Type/version supplies native TUI/Chat mode, instructions and Skills;
the start body cannot override role, actor or configuration. The dedicated native
role is `agent_manager`, separate from workers and the Orchestrator. Starting a
controller does not itself select or launch a task worker.

The definition requires schema 1, `enabled`, an exact `agentTypeId` and positive
`agentTypeVersion`, and explicit `policy`. Policy has `optimization` (quality,
balanced, speed or usage), independent `allowCreateTypes`, `allowCreateSkills`,
`allowCreateVersions` flags, and bounded `maxCreatedTypes` (0–32),
`maxCreatedSkills` (0–64), `maxVersionsPerEntry` (0–32), `maxPendingRequests`
(1–1000) and `maxProposalAttempts` (1–5). An enabled creation flag requires a
positive corresponding quota. Entry-level permissions also govern future actions.

History remains inspectable after Type edits/disable. Page limits are 1–100;
continue with returned `nextCursor`. Inspect the retained Type content hash,
policy, author, reason and timestamp rather than inferring them from the live Type.

`enqueue` accepts `id` (a stable retry key), `taskId`, positive `taskRevision`,
`configurationVersion` (1–1000), and `reason`. Example:

```json
{"id":"route-task-1","taskId":"task-1","taskRevision":1,"configurationVersion":1,"reason":"Choose a permitted worker for this task"}
```

The task must belong to this project, have frozen criteria and current run intent;
the Manager must be enabled at the specified configuration. Queue limits and one
pending request per task are checked atomically. Retrying the same ID and fields
returns the retained request; changed fields conflict. IDs are at most 200 bytes
without control characters; reasons are nonempty and at most 2000 bytes. These
inbox write bodies are limited to 16 KiB. The request references exact versions
and hashes. After startup reconciliation, the daemon consumes eligible inbox work
and admits the configured native Manager if none is reserved. Configuration edits
alone do not start a controller. Failed/uncertain admission stays reserved; repeated
queue scans do not create a replacement process.

`inbox` returns pending requests; `requests` includes terminal history. Reads do
not acknowledge native delivery. `request-resolution` returns `resolution: null`
while pending. `resolve` accepts `outcome` (`cancelled`, `superseded`, `needs_human`)
and `reason`, retaining an immutable receipt. It closes only routing intent; it
does not cancel a task, stop a worker, release ownership or claim successful
selection. Governance/inbox writes derive authority from the caller's daemon
service context; actor, origin and session fields are not accepted in JSON.

The native `propose` command uses its Manager session path and accepts an envelope
with `sourceGeneration`, `idempotencyKey` and `raw`. The generation must be the
current confirmed Manager generation; workers cannot use this role. `raw` retains
the exact output, including empty/malformed output, up to 64 KiB; the JSON envelope
limit is 512 KiB to accommodate escaping. Actor, project, controller and source-owner
fields are never supplied by the model. Example inner output (encode as `raw`):

```json
{"schemaVersion":1,"action":"select_existing","agentTypeId":"permitted-type","agentTypeVersion":1,"rationale":"Explain candidates and tradeoffs","candidates":[]}
```

The alternative action `needs_human` requires a rationale and no selected Type.
Candidate explanations contain exact `agentTypeId`, `version` and `reason`, at
most 32 entries; they are claims for deterministic validation. The receipt retains
the proposal and includes `decision` when assessment completed. `accepted` records
routing selection; worker admission remains the scheduler's responsibility.
`rejected` includes candidate exclusion codes. Malformed inner output is retained
with `validationError`. Parser and semantic failures share the original correction
budget; exhaustion closes routing as Needs Human. A semantic rejection permits a
changed correction under a new key. Exact retries inspect retained output and its
first decision; changed bytes conflict. `routingOutcome` is a terminal code, never
the caller's unclassified resolution reason. Stop correcting a terminal request.

`decisions` returns at most five immutable assessments; `decision` uses a proposal
ID and returns `decision: null` if none is retained. History remains available
after policy changes or native termination. The daemon recovers unassessed parsed
output after restart through the same deterministic service. Native protocol v3
supplies feedback/history commands and bounded retry instructions, preserving the
original v1/v2 bytes. Interrupted responses do not prove that persistence failed.

`contexts` lists at most 32 immutable routing inputs; `context` reads exact bytes,
per-item classification, pinned clearance, native generation, tool instructions
and the preceding conversation hash. `deliveries` lists at most four native send
attempts. `handed_off` means transport acceptance; it does not prove that the agent
read the input or that a selection succeeded. `uncertain` and interrupted sends
hold the conversation for reconciliation. Do not resend them as a new request or
assume resolving routing intent clears the native composer. Four proven no-send
attempts become Needs Human. Unknown readiness consumes no attempt.

Each input's native CLI instructions retain the exact source generation, literal
argv and run-file environment. Use those instructions rather than reconstructing
commands from live session state. New proposals require an attributed input and
record cumulative conversation classification, including earlier sensitive work.
A Manager conversation retains its first engagement binding across requests and
native generations. A task above its pinned clearance, or another engagement,
becomes Needs Human without embedding the prohibited context. Other eligible
projects continue through the bounded inbox scan.
