# Agent Type and Skill registry

`ao agent-type` and `ao skill` call the daemon registry API. Both output JSON.
Plural aliases are `agent-types` and `skills`. These author reusable definitions;
they do not start workers. Native provider skill discovery remains separate.

```sh
ao agent-type list --limit 100
ao agent-type show <id>
ao agent-type versions <id>
ao agent-type audit <id>
ao skill list
ao agent-type export <id> <version>
ao skill export <id> <version>
```

List/history responses include `nextCursor` when another page may exist. Pass
that value as `--cursor` to continue. History preserves disabled definitions.

Writes accept one API JSON object through `--file path.json` or `--file -` for
stdin. Required fields and types are available at `/api/v1/openapi.yaml`.

| Command | Request fields |
| --- | --- |
| `create` | `metadata`, `definition`, `reason` |
| `new-version <id>` | `definition`, `expectedRevision`, `reason` |
| `update <id>` | `metadata`, `expectedRevision`, `reason` |
| `activate <id>` | `version`, `expectedRevision`, `reason` |
| `clone <id>` | `version`, `name`, `reason` |
| `import` | `bundle`, `reason` |

Metadata contains `name`, `description`, `enabled` and `policy` with three
independent booleans: `managerCanSelect`, `managerCanModify`, `managerCanVersion`.
Setting `enabled` to false disables future selection without deleting history.

Definitions contain exactly one of `agentType` or `skill`. An Agent Type contains
an AO harness id, existing `config`, `instructions`, `capabilities`, ordered
`skills` references (`id` + exact `version`), and `maxParallelWorkers`. A Skill
contains `instructions`, `capabilities`, `requiredTools`, `requiredMcpServers`,
and bounded inert `resources` (`path` + `content`). Resources cannot traverse
directories or replace the skill's main instructions.

New versions are inactive until explicitly activated. Rollback activates an
older version; it does not overwrite history. Read the current revision before
updating. A conflict means reload and reconcile the intended changes, never
blindly retry with a guessed revision.

Actor and origin cannot be supplied in request JSON. These authoring CLI routes
are human actions under AO's trusted local-host model. A managed agent uses its
scoped manager tools for policy-checked actions; it must not relabel a manager
proposal as a human registry action. Never put credentials in definitions.

Export returns a schema-versioned portable bundle (maximum 1 MiB), including
the exact content of pinned Skills and their inert resources. Local identities,
actors, account references and provider binding IDs are excluded. A required
custom binding becomes `requiresProviderBinding: true`, which must be resolved
locally before launch. Authored instructions/resources are included verbatim.

To import, wrap the exported JSON as `{"bundle": <exported object>, "reason":
"why this is useful"}` and pass it through `--file`. Unknown fields and schema
versions are rejected. A successful import creates all new Skill/type identities
in one transaction, disabled and with all manager permissions off. Review and
enable the imported Skills and root explicitly; importing never installs tools,
executes resources, changes authentication, or grants a requested capability.
