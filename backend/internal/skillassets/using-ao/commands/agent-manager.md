# Agent Manager governance

`ao agent-manager` uses the daemon API and returns JSON.

```bash
ao agent-manager show <project>
ao agent-manager configurations <project> --limit 20
ao agent-manager configuration <project> <version>
ao agent-manager audit <project> --cursor 0 --limit 20
ao agent-manager configure <project> --file manager.json
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
