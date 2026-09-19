# `ao project` — deterministic controls, Needs Human and dry-run

`ao project` grew the stage-20 control family. All commands are thin
HTTP clients over the daemon and print the daemon's JSON envelope.

## Control state

Every project has exactly one adaptive control state:

| State | Admissions | Meaning |
| --- | --- | --- |
| `running` (default) | open | The absence of a control row is the durable default |
| `paused` | fenced | Current tasks continue; pending work is retained |
| `draining` | fenced | Lets current attempts finish; reads as paused once none remain |
| `stopped` | fenced | Also fences follow-up dispatch; resume explicitly |

```bash
ao project control <project>                 # read stored + effective state
ao project pause <project> --reason "why"    # running -> paused
ao project resume <project> --reason "why"   # -> running (also ends a drain)
ao project drain <project> --reason "why"    # running -> draining
ao project stop <project> --reason "why"     # -> stopped
```

Fencing is enforced inside the session-creation and task-reservation
transactions, so no launch caller (manual spawn, delegation, review launcher,
Manager admission, tracker observer) can slip past a pause that already
returned. Standalone projectless workers and other projects are never fenced.
Refusals surface as typed `PROJECT_ADMISSIONS_FENCED` envelopes.

## Cancel work

```bash
ao project cancel <project> --scope pending --reason "why"  # unleased work only
ao project cancel <project> --scope all --reason "why"      # also marks leased work cancelling
```

Cancel-all additionally requests termination of every live worker holding a
task attempt through the normal kill services and reports each request;
cancellation never assumes a worker stopped.

## Needs Human

```bash
ao project needs-human <project> [--after <id>] [--limit 20]
ao task needs-human <task-id> --code credential_missing --detail "what is missing"
ao task resolve-human <task-id> --resolution "the recorded decision"
```

Reason codes: `credential_missing`, `approval_required`, `ambiguous_intent`,
`provider_unavailable`, `recovery_inconclusive`. A pending request blocks new
attempts on its task and its descendants only; unrelated DAG branches stay
schedulable, and the task read projection shows the `needs-human` phase.

## Dry-run

```bash
ao project dry-run <project> --file plan.json   # use - for stdin
```

The plan file uses the exact actions the sealed orchestrator protocol
submits (`create_task`, `revise_task`, `freeze_criteria`). The daemon
simulates validation, selection and admission against current durable state
with writes and process launches disabled — no worktree, no harness install,
no repository write, no registry/task mutation. The verdict reports
per-action findings, requested-worker resolution, the combined graph
validity, current scheduler headroom and the project control state. Usage
and cost estimates are reported as `unknown`, never invented.
