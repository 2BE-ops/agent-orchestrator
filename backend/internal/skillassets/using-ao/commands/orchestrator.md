# ao orchestrator

Manage orchestrator sessions.

## Syntax

```
ao orchestrator <subcommand> [flags]
```

## Subcommands

---

### ao orchestrator ls

List orchestrator sessions. Aliases: `ls`, `list`.

**Syntax:**
```
ao orchestrator ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |

## Examples

```bash
# List all orchestrator sessions
ao orchestrator ls
```

```bash
# List orchestrator sessions as JSON
ao orchestrator ls --json
```

---

### ao orchestrator goal

Read the project's current goal plus the live orchestrator generation. This is
the first command of every native planning cycle: submissions must carry the
returned `sourceGeneration` exactly, and AO rechecks it against the project's
live orchestrator session.

**Syntax:**
```
ao orchestrator goal <project>
```

### ao orchestrator set-goal

Record a user-authored goal version. Goal wording is human authority: the
orchestrator plans against it and completes it but never rewrites it. The JSON
body contains `goal` and `reason`.

**Syntax:**
```
ao orchestrator set-goal <project> --file goal.json
```

```json
{"goal":"Ship the reporting milestone with independent verification","reason":"Kick off the adaptive loop"}
```

### ao orchestrator goal-versions / completions

Page immutable goal history and retained verified completions with `--after`
and `--limit` (1–100).

### ao orchestrator plan

Submit exactly one native planning action from JSON: `create_task`,
`revise_task` or `freeze_criteria`. AO mints task identity; the graph,
dependencies and criteria validation all run transactionally, and each action
is sealed in an immutable receipt. Keep one stable `idempotencyKey` per
identical action; a changed action uses a new key. An exact retry returns the
original receipt without re-running anything.

**Syntax:**
```
ao orchestrator plan <project> --file plan.json
```

```json
{
  "sourceGeneration": "REPLACE_WITH_GOAL_SOURCE_GENERATION",
  "idempotencyKey": "plan-1",
  "action": {
    "action": "create_task",
    "reason": "Decompose the verified goal",
    "definition": {
      "title": "Add reporting endpoint",
      "brief": "Implement the endpoint with tests",
      "category": "feature",
      "priority": 5,
      "maxAttempts": 3,
      "dependencies": [],
      "requiredCapabilities": []
    }
  }
}
```

`revise_task` adds `taskId` and `expectedRevision`; `freeze_criteria` replaces
`definition` with `criteria`. Do not spawn workers or choose agents here:
delegation belongs to the Agent Manager and admission to the scheduler.

### ao orchestrator complete

Submit a goal-completion assessment. Completion is deterministic: AO verifies
every project task is independently verified against its frozen criteria or
explicitly cancelled before retaining the decision, and otherwise refuses with
the concrete blockers. The body contains `sourceGeneration`, `goalVersion`,
`summary` and `reason`.

**Syntax:**
```
ao orchestrator complete <project> --file complete.json
```

### ao orchestrator feedback

Read derived per-task loop facts (`pending`, `working`, `completed`, `failed`
for exhausted attempts, `cancelling`, `cancelled`) with `--after` and
`--limit`. Nothing is stored: the same read after a restart returns the same
answers from durable rows.

### ao orchestrator receipts / receipt

Page or inspect sealed native planning receipts with `--after`/`--limit`
(1–100). Receipts survive restart and are immutable.

### ao orchestrator planning-outcomes / planning-summary

`planning-outcomes` pages sealed planning receipts, each coupled with the
derived fate of its task (`pending`, `working`, `completed`, `failed`,
`cancelling`, `cancelled`), with `--after`/`--limit` (1–100).
`planning-summary --from --to` (RFC3339, at most a 366-day window) aggregates
one complete cohort: receipts by action (create/revise/freeze), created-task
states and attempts across planned tasks. Windows beyond 1000 receipts are
refused rather than partially summed. These are planning metrics — the plan
author's outcomes — distinct from worker performance.
