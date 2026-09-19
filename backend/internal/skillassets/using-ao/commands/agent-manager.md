# Agent Manager governance and routing inbox

`ao agent-manager` uses the daemon API and returns JSON.

```bash
ao agent-manager show <project>
ao agent-manager configurations <project> --limit 20
ao agent-manager configuration <project> <version>
ao agent-manager audit <project> --cursor 0 --limit 20
ao agent-manager configure <project> --file manager.json
ao agent-manager inbox <project> --cursor 0 --limit 20
ao agent-manager requests <project> --cursor 0 --limit 20
ao agent-manager request <project> <request>
ao agent-manager request-resolution <project> <request>
ao agent-manager enqueue <project> --file routing.json
ao agent-manager resolve <project> <request> --file resolution.json
```

`configure` is a human governance action. Manager tools cannot grant themselves
policy permissions. Its API JSON includes `definition`, `expectedRevision` and
`reason`; origin/actor fields are not accepted. Use revision 0 only for initial
configuration, then the current configuration's `number`. A stale edit conflicts.
Configuration edits do not launch a controller or alter running configuration.

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
inbox write bodies are limited to 16 KiB. Neither enqueue nor configuration starts
a native controller. Task content is referenced by exact versions and hashes.

`inbox` returns pending requests; `requests` includes terminal history. Reads do
not acknowledge native delivery. `request-resolution` returns `resolution: null`
while pending. `resolve` accepts `outcome` (`cancelled`, `superseded`, `needs_human`)
and `reason`, retaining an immutable receipt. It closes only routing intent; it
does not cancel a task, stop a worker, release ownership or claim successful
selection. Governance/inbox writes derive authority from the caller's daemon
service context; actor, origin and session fields are not accepted in JSON.
