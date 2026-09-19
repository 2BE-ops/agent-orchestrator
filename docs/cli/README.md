# AO CLI

The `ao` CLI is a thin Go/Cobra client for the local Agent Orchestrator daemon.
It starts, discovers, inspects, and stops the daemon through the loopback HTTP
surface and the `running.json` handshake. It must not open SQLite directly or
call runtime, workspace, tracker, or agent adapters in-process.

When using the CLI directly from a shell, make sure the daemon is running first
with `ao start` or by opening the desktop app. Product commands such as
`ao agent ls` and `ao spawn` call the loopback daemon and will fail with a
"daemon is not running" error if no `running.json` points at a live process. From
a source checkout, build and run the local binary explicitly, for example:

```bash
cd backend
go build -o ./bin/ao ./cmd/ao
./bin/ao agent ls
```

## Current commands

Every product command resolves to a daemon HTTP route. Run `ao <command>
--help` for the authoritative flag shape.

### Daemon control

| Command                       | Purpose                                                                                                                           |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `ao start`                    | Start the daemon in the background and wait for `/readyz`.                                                                        |
| `ao stop`                     | Gracefully stop the daemon via loopback `POST /shutdown` after verifying daemon identity.                                         |
| `ao status` / `--json`        | Report daemon state from `running.json`, process liveness, `/healthz`, and `/readyz`.                                             |
| `ao doctor` / `--json`        | Check config, data directory, DB-file presence, daemon state, `git`, and (on Darwin/Linux) `tmux`; on Windows conpty is built in. |
| `ao completion <shell>`       | Generate completions for `bash`, `zsh`, `fish`, or `powershell`.                                                                  |
| `ao version` / `ao --version` | Print build metadata.                                                                                                             |
| `ao daemon`                   | Hidden internal daemon entrypoint used by `ao start`.                                                                             |

### Product commands

| Command                             | Daemon route                                   |
| ----------------------------------- | ---------------------------------------------- |
| `ao project add`                    | `POST /api/v1/projects`                        |
| `ao project ls`                     | `GET /api/v1/projects`                         |
| `ao project get <id>`               | `GET /api/v1/projects/{id}`                    |
| `ao project set-config <id>`        | `PUT /api/v1/projects/{id}/config`             |
| `ao project rm <id>`                | `DELETE /api/v1/projects/{id}`                 |
| `ao agent ls`                       | `POST /api/v1/agents/readiness/ensure` (`display`) |
| `ao agent ls --refresh`             | `POST /api/v1/agents/refresh` (forced checks) |
| `ao spawn`                          | Targeted launch ensure, then `POST /api/v1/sessions` |
| `ao session ls`                     | `GET /api/v1/sessions` plus per-session PR summaries; shows branch, PR, CI, review, unresolved threads, activity, and age. |
| `ao session get <id>`               | `GET /api/v1/sessions/{id}`                    |
| `ao session kill <id>`              | `POST /api/v1/sessions/{id}/kill`              |
| `ao session restore <id>`           | `POST /api/v1/sessions/{id}/restore`           |
| `ao session exit-agent <id>`        | `POST /api/v1/sessions/{id}/exit-agent`        |
| `ao session resume-agent <id>`      | `POST /api/v1/sessions/{id}/resume-agent`      |
| `ao session switch-agent <id> <target-harness>` | `POST /api/v1/sessions/{id}/switch-agent` |
| `ao session agent-switch ls <session-id>` | `GET /api/v1/sessions/{id}/agent-switches` |
| `ao session handoff submit`         | `POST /api/v1/sessions/{id}/agent-switches/{switchId}/handoff` |
| `ao session rename <id> <name>`     | `PATCH /api/v1/sessions/{id}`                  |
| `ao session cleanup`                | `POST /api/v1/sessions/cleanup`                |
| `ao session claim-pr [<id>] <pr-ref>` | `POST /api/v1/sessions/{id}/pr/claim`        |
| `ao orchestrator ls`                | `GET /api/v1/orchestrators`                    |
| `ao send`                           | `POST /api/v1/sessions/{id}/send`              |
| `ao preview [url]`                  | `POST /api/v1/sessions/{id}/preview`           |
| `ao preview start/status/stop`      | `POST/GET/DELETE /api/v1/sessions/{id}/preview/server` |
| `ao browser ...`                    | `GET /api/v1/browser/status`, `POST /api/v1/browser/commands` |
| `ao hooks <agent> <event>`          | `POST /api/v1/sessions/{id}/activity` (hidden) |

### Project knowledge

`ao knowledge list <project> --status accepted --search "service boundary"`
searches current knowledge; `show <id>`, `versions <id>` and
`version <id> <number>` inspect provenance and immutable review history.
`create <project> --file <path>` accepts `definition` and `reason`; `revise <id>
--file <path>` also requires `expectedVersion`. Definitions include `title`,
`kind`, `content`, `status`, `confidence`, `pinned` and `sources`. A source has
`kind` and `reference`, plus task/attempt/session or content/commit hashes when
applicable. File/stdin requests are bounded to 128 KiB.

Review status is `candidate`, `accepted`, `invalidated`, `superseded` or `deleted`.
Only accepted knowledge can be pinned. Set `deleted` in a revision to withdraw
future selection while retaining exact historical versions for provenance.
Superseded knowledge must reference an accepted exact `supersededBy` ID/version.

### Persistent task planning

`ao task show <id>` includes derived planning/execution state, cancellation
intent and retained lease facts. `ao task intents <id>` pages control history.
`ao task set-intent <id> --file <path>` accepts `intent` (`run` or `cancel`),
`expectedRevision`, `expectedVersion` (zero initially), and `reason`. Cancellation
blocks new admissions for the task and its descendants; active ownership remains
reserved for lifecycle cleanup. A `cancelling` state is not proof a worker stopped.

`ao task context <task-id> <attempt-id>` inspects the exact sealed prompt,
versioned sources, hashes, byte/token estimates and omission reasons. It returns
404 until context is sealed; inspection never launches work or reads files.
Task definitions may include up to 16 portable workspace-relative `contextFiles`.
The builder combines frozen task/criteria/dependencies, parent planning, explicit
regular text files and up to 32 accepted relevant knowledge records. Pinned
knowledge ranks before task links, category tags and general project facts.
Candidate, invalidated and unrelated claims are excluded. Known credential paths,
symlinks, unavailable, oversized and binary files are recorded as omitted.
Type/Skill content remains linked to the exact worker configuration and resources.
Default inline input is limited to 192 KiB and 48 Ki estimated tokens including
system instructions; token estimates use four UTF-8 bytes per token. Restoration
retains the original context after knowledge or file changes.

`ao task submit-result <session-id> --file <path>` submits a JSON request with
`sourceGeneration`, a stable `idempotencyKey`, `expectedVersion` (zero initially)
and `definition`. The source generation must match the submitting worker's native
execution. The daemon derives its attempt, actor, configuration and context; these
cannot be supplied as author tags. Request JSON is limited to 512 KiB; the result
definition is limited to 256 KiB and 16 corrections per attempt. Reuse a key only
for an exact retry; corrections use a new key and the last result version.

Reserved TUI and Chat workers receive executable/argv and JSON examples in their
sealed launch context. These instructions count toward the prompt budget and
retain the reserved native generation. Ownership conflicts preserve output for
recovery; historical instructions cannot refresh themselves to a new owner.

A definition has `schemaVersion: 1`, `claimedOutcome` (`completed`, `partial` or
`blocked`), `summary` and `implementation`, plus optional `claimedCommit` (full Git
object ID) and the collections `decisions`, `assumptions`, `interfaces`, `tests`,
`findings`, `unresolvedIssues`, `recommendedFollowUp` and `knowledgeCandidates`.
Tests contain `command`, `outcome` (`passed`, `failed`, `not_run`, `unknown`) and
`details`; interfaces contain `name`, `contract` and workspace-relative `files`.
Knowledge candidates contain `title`, `kind`, `content`, `confidence` and `tags`.
These are worker claims awaiting independent evaluation; commands are not executed
and knowledge is not automatically accepted. `ao task results <task-id>
<attempt-id>` pages claim history; `ao task result <task-id> <attempt-id>
<result-id>` reads an exact submission. Malformed submissions leave native output
and previous results intact.

`ao task evaluate <task-id> <attempt-id> --file <path|->` collects an immutable
assessment of independent evidence. Its JSON request contains `resultId`,
`expectedVersion` (zero initially), a stable `idempotencyKey`, and `reason`, with an
8 KiB limit. Criteria, evidence, author identity and verdict are derived by the
daemon. An exact retry returns the original snapshot; a fresh assessment requires
a new key, the latest evaluation number, and the attempt's latest result.
`ao task evaluations <task-id> <attempt-id>` pages up to 64 assessments per attempt;
`ao task evaluation <task-id> <attempt-id> <evaluation-id>` inspects exact evidence,
source timestamps, frozen criteria, and Type/Skill/model/configuration attribution.

CI criteria use `evidenceKind: "ci"` and one or more exact `checkNames`.
The `test`, `build` and `lint` kinds can also freeze exact `checkNames` to classify
their independently observed CI results. Only
successful checks observed in the retained PR snapshot at the result's exact full
commit can pass. Missing, pending, cancelled, skipped, mismatched, or truncated
evidence is inconclusive. Worker-reported tests cannot satisfy these criteria.
Command-only criteria retain their original description but remain inconclusive
without supported independent evidence. A check selector and command vector cannot
be combined; the assessment does not claim the daemon ran a CI job's command.
`mergeability` criteria require a non-draft PR observed at the exact result commit
with a known mergeable state. An observed conflict or closed unmerged PR fails;
unknown, stale or truncated observations cannot pass. Other criterion kinds remain
inconclusive until their independent collectors are available. Assessment reads do
not refresh SCM data or change leases or planning.

An `artifact` criterion can freeze `artifactPath` and `artifactSha256`. Collection
reads the raw regular Git blob at the result's full commit from the worker's
repository and compares its SHA-256 with the frozen expectation. Dirty files,
checkout/export filters and worker-reported hashes are excluded. A missing path or
hash mismatch fails; missing commits, oversized blobs, symlinks, submodules and
unavailable Git remain inconclusive. Collection reads at most 16 artifacts, each
at most 1 MiB, within a 15-second total deadline. It does not fetch missing objects,
execute artifact content or modify the repository. Legacy artifact criteria without
a frozen hash remain inconclusive. Exact evaluation retries return stored evidence
before accessing Git, even after the repository becomes unavailable.

Assessments also retain up to 16 PR observations, 32 latest review-run references
at the target commit, and worker activity/termination/lease facts. Review previews
are capped at 4 KiB with a preview hash, original byte count and truncation flag.
Review verdicts are qualitative evidence; a generic approval cannot satisfy frozen
task criteria. Reservation elapsed time includes waiting and is explicitly marked
ongoing until lease release. Termination alone does not identify a worker crash.

`ao task send-message <session-id> --file <path|->` persists structured coordination.
Its request contains `sourceGeneration`, a stable `idempotencyKey`, and a
`definition` with `schemaVersion: 1`, `kind`, `targetTaskId`, `subject`, `body` and
`correlationId`. Supported kinds are `finding`, `question`, `answer`, `blocker`,
`handoff`, `interface_contract`, `review_request` and `dependency_update`. A target
must be another task in the same project. It need not have a worker yet. Replies
include `replyToId` and retain the original thread and task pair; an answer must
reply to a question. An optional `resultId` must belong to the sending attempt.
An `interface_contract` includes an `interface` object with `name`, `contract` and
workspace-relative `files`. All content remains attributed worker claims.

Message requests are capped at 64 KiB, definitions at 32 KiB, history at 256 messages
per attempt and 10,000 per project. Exact retries return the same stored message.
`ao task messages <project> [--task <task-id>] [--cursor <sequence>] [--limit <n>]`
reads the shared timeline; the task filter includes both sent and received messages.
`ao task message <project> <message-id>` reads exact content and delivery history.
An empty delivery history means no native send has been reserved. `dispatching`
means reserved, `handed_off` means transport acceptance, `not_sent` means proven
undelivered and `uncertain` requires reconciliation. These observations never mean
the recipient has read the message or accepted a proposed interface. Only proven
undelivered messages permit another automatic send, with at most four attempts.

`ao task` (alias `ao tasks`) authors work independently of worker sessions and
returns JSON. `create <project> --file <path>` accepts the task API body:

```json
{
  "definition": {
    "title": "Add regression coverage",
    "brief": "Cover the reported failure and retain verification evidence.",
    "category": "testing",
    "priority": 0,
    "dependencies": [],
    "requiredCapabilities": [],
    "maxAttempts": 3
  },
  "criteria": {
    "criteria": [{
      "id": "regression",
      "requirement": "The regression test passes against the implementation.",
      "evidenceKind": "test",
      "command": ["go", "test", "./..."]
    }]
  },
  "reason": "Plan the requested regression fix"
}
```

Use `--file -` for stdin. Input is one JSON object, bounded to 256 KiB. Optional
`definition.requestedWorker` uses the same `agentTypeId`, `version` and `overrides`
as manual worker launch. Criteria may be omitted while planning but must be
persisted before leasing. Commands in criteria describe expected verification;
authoring a task does not execute them or launch a worker.

Use `list <project>`, `show <id>`, `revisions <id>`, `revision <id> <version>`,
`criteria <id> <version>`, `audit <id>` and `attempts <id>` to inspect work.
List/history commands support `--cursor` and `--limit 1..100` (default 20).
`revise <id> --file <path>` accepts `definition`, `expectedRevision` and `reason`;
`set-criteria <id> --file <path>` accepts `criteria`, `expectedRevision` and
`reason`. Both append history. Existing attempts retain their frozen task and
criteria versions. Actor identity is assigned by the daemon.

`ao agent ls` asks the daemon to ensure display readiness, then prints the
existing table or legacy JSON projection. The daemon alone decides whether a
native check is needed. `--refresh` is a deprecated compatibility flag that
forces fresh installation and authentication checks before printing.

`ao spawn` resolves project context in this order: explicit `--project`,
`AO_PROJECT_ID`, `AO_SESSION_ID` (by fetching the current session from the
daemon), then the current working directory matched against registered project
paths. If `AO_SESSION_ID` is set but the session cannot be fetched, pass
`--project` explicitly. Use `ao spawn --standalone --agent <agent> --name
<name>` to launch a worker in an AO-managed plain directory without resolving
or registering a project. Standalone sessions do not support orchestrator,
branch, issue, or PR-claim options.

Agent switching is initially available only for worker sessions whose source
and target harnesses are Claude Code or Codex. The main command
accepts an idempotency key:

```bash
ao session switch-agent ao-7 codex \
  --idempotency-key switch-ao-7-to-codex

ao session agent-switch ls ao-7 --json
```

`switch-agent` and `agent-switch ls` both support `--json`.
The `agent-switch` command also has the `agent-switches` alias, and `ls` has the
`list` alias.

`ao session handoff submit` is the internal source-agent path for optional
semantic enrichment, not a required human step in a normal switch. It requires
the switch ID, exact source launch generation, and a regular file containing
one JSON object no larger than 64 KiB. `--session` defaults to
`AO_SESSION_ID`:

```bash
AO_SESSION_ID=ao-7 ao session handoff submit \
  --switch switch-123 \
  --source-generation generation-456 \
  --file /tmp/ao-handoff.json \
  --json
```

Switching preserves the AO worker session and worktree. It does not translate,
clip, or rewrite provider transcript files; providers continue to own their
native history and compaction.

`ao session claim-pr <pr-ref>` attaches a PR to the current worker by reading
`AO_SESSION_ID`. From an orchestrator or external shell, pass the target
explicitly with `ao session claim-pr <session-id> <pr-ref>`. The explicit form
remains supported for backward compatibility and cross-session coordination.

If `--agent` / `--harness` is omitted, `ao spawn` uses the resolved project's
`worker.agent` config. Before spawning, the CLI performs one targeted launch
ensure. It fails early for unsupported or definitely missing harnesses and
warns-but-continues for unauthorized or unknown observations; daemon session
creation repeats launch validation and native launch remains authoritative.
`--skip-agent-check` suppresses only the CLI warnings and early check, never the
daemon validation.

Standalone spawns require `--agent` because there is no project configuration
from which to resolve a default harness.

`ao preview` resolves its session from the `AO_SESSION_ID` environment variable
(it is meant to run inside a session), not a flag. With no argument it
autodetects an `index.html` in the session workspace. Relative file targets are
resolved from the session workspace root, regardless of the shell's current
directory, and served through AO's confined loopback preview origin. Absolute
paths and `file://` URLs must resolve inside that workspace; explicit HTTP and
HTTPS targets remain regular browser URLs.

`ao preview start [configuration]` loads `.ao/launch.json` from the session
workspace, starts that exact command under a session-owned supervisor, selects
or records its loopback port, waits for readiness, and opens application
targets in the Browser panel. `status` reports bounded recent logs and `stop`
terminates the managed process tree. Multiple configurations must be selected
by name; AO does not assign confidence scores to arbitrary localhost servers.
This is an optional, reusable project configuration, not a prerequisite for
preview. Agents must not create it automatically. Static HTML and Markdown use
the direct file preview and must not cause package-manager scaffolding,
dependency installation, or a development server to be introduced.

When a browser-displayable file is the requested artifact, agents should call
`ao preview <workspace-path>` immediately after creating or materially updating
the primary output. Markdown, HTML, PDF, SVG, and common images can be served
directly. Supporting assets must not replace an active application preview.

`ao browser` also resolves its target from `AO_SESSION_ID`, but controls the
session-owned live Electron browser rather than only setting its preview URL.
The target-isolated command set includes `status`, `open`, `snapshot`, `click`,
`dblclick`, `focus`, `fill`, `type`, `press`, `hover`, `scroll`,
`scrollintoview`, `drag`, `select`, `check`, `uncheck`, `get`, `highlight`,
`unhighlight`, `tabs`, `tab new`, `tab select`, `tab close`, `frame`, `dialog`,
`wait`, `screenshot`, `network start/status/list/stop/clear`, `console`, and
`errors`. The native engine is bound internally; there is no second command or
connection setup. Logical tab IDs remain stable for the session, and allowed popups
become AO browser tabs rather than separate OS-browser windows. The AO desktop
app must be open because Electron owns the `WebContentsView`.
References from a snapshot are invalidated after navigation or DOM replacement;
they are also invalidated when changing tabs. Take another snapshot when a
command reports `STALE_REFERENCE`.
Browser waits cover load completion, text or selector appearance and
disappearance, URL matching, fixed delays, and a configurable DOM-stability
window for HMR-driven verification.
Browser tabs in the same worker share a memory-only Electron profile. Different
workers receive distinct partitions, so cookies, authentication, local storage,
and session storage do not leak between their browser runtimes.
Network capture is disabled by default and must be started explicitly. It is
scoped to the active tab at start time, expires after 60 seconds by default
(maximum 300), retains at most 200 in-memory entries, and is cleared with the
tab/session. Captured data is metadata-only: request and response bodies are
never read, sensitive headers are omitted, and URL credentials, fragments, and
query values are redacted.

`go run .` in `backend/` remains a compatibility wrapper around the daemon.

PR actions are available through `ao pr merge` and
`ao pr resolve-comments`. Review actions are available through `ao review ls`,
`ao review trigger` (also `execute` and `restart`), `ao review cancel` (also
`stop`), and `ao review submit`.

## Configuration

The CLI and daemon share the same environment-driven config:

| Var                   | Default              | Purpose                                                                                        |
| --------------------- | -------------------- | ---------------------------------------------------------------------------------------------- |
| `AO_PORT`             | `3001`               | Loopback daemon port.                                                                          |
| `AO_RUN_FILE`         | `~/.ao/running.json` | PID/port handshake.                                                                            |
| `AO_DATA_DIR`         | `~/.ao/data`         | SQLite data directory.                                                                         |
| `AO_REQUEST_TIMEOUT`  | `60s`                | REST request timeout.                                                                          |
| `AO_SHUTDOWN_TIMEOUT` | `10s`                | Graceful shutdown cap.                                                                         |
| `AO_KEEP_DAEMON`      | unset (off)          | Keep the desktop app's daemon running after the window closes; stop only via `ao stop`. (fork) |
| `AO_DISABLE_GPU`      | unset (off)          | Skip Chromium hardware acceleration; escape hatch for broken Linux GPU drivers.                |

The daemon always binds `127.0.0.1`.

## Task review requests and evidence

An attempt's acceptance criteria can pin `reviewPolicy.agentTypeId` and `version`,
with `differentAgentType` and `differentHarness` requirements. Request review of
an exact submitted result using a JSON file containing `{"resultId":"..."}`:

```bash
ao task request-review <task-id> <attempt-id> --file review-request.json
ao task reviews <task-id> <attempt-id> <result-id>
ao task review <task-id> <attempt-id> <run-id>
```

The daemon resolves that pinned Type and its Skills, seals the criteria and native
configuration, and launches through the existing reviewer service. History is
bounded to 64 passes per result; inspection shows the retained payload and native
launch witness even after registry edits. Active and approved scopes are reused.
An uncertain launch remains reserved for reconciliation. New admission still
requires the pinned Type and native configuration to be available.

The native reviewer's submission includes `sourceGeneration` in each batch item,
or `--source-generation` for a single `ao review submit`. A verdict is qualitative
review evidence; independent task evaluation remains a separate operation.

## Agent Manager governance

`ao agent-manager configure <project> --file manager.json` versions desired
human-owned policy through the daemon. It does not launch a controller or change
a running controller's pins. `show`, `configurations`, `configuration <project>
<version>` and `audit` inspect the current policy and immutable history as JSON.
History/audit accept `--cursor` and `--limit` (1–100).

`start <project> --file start.json` explicitly starts the configured native Manager
with a stable `id`, exact `configurationVersion` and `reason`. An exact retry reads
retained admission and never authorizes another native launch. `current <project>`
reports reserved ownership (or null); `controller <project> <controller-id>` reads
an exact retained admission and session binding. A pending native operation requires
reconciliation, not another start ID. Native mode and configuration come from the
pinned Type; starting the controller does not itself launch task workers.

The request contains `definition`, `expectedRevision` (0 only initially) and a
reason. The definition pins an exact Agent Type version and carries explicit,
bounded creation/inbox/retry policy. Creation is opt-in; entry-level ownership
permissions remain independent. Stale edits return a conflict. See the embedded
[Manager command contract](../../backend/internal/skillassets/using-ao/commands/agent-manager.md)
for all fields and bounds. Governance edits are human actions; the Manager's
structured tools cannot escalate their own permissions.

`inbox <project>` pages pending routing requests; `requests <project>` includes
terminal history. `request <project> <request>` inspects exact task, criteria and
policy references/hashes; `request-resolution` shows a retained receipt or null.
`enqueue <project> --file routing.json` records a stable retry ID, task ID/revision,
governance version and reason. `resolve <project> <request> --file resolution.json`
records cancelled, superseded or Needs Human with a reason. Both writes use bounded
16 KiB JSON and server-derived authority. They affect routing intent only: native
launch, task cancellation and ownership release use their own service boundaries.

`propose <session-id> <request> --file proposal-envelope.json` submits a native
Manager's `sourceGeneration`, `idempotencyKey` and exact `raw` output. It preserves
malformed/empty output for bounded correction and rejects stale or unrelated native
sources. Receipts acknowledge persistence, not applied selection. `proposals
<project> <request>` and `proposal <project> <request> <proposal>` expose the retained
output/parser history independently of live ownership. The embedded contract above
documents the versioned inner protocol and envelope bounds.

## Task performance evidence and metrics

```bash
ao task performance <project> --from 2026-09-01T00:00:00Z --to 2026-10-01T00:00:00Z --limit 20
ao task metrics <project> --from 2026-09-01T00:00:00Z --to 2026-10-01T00:00:00Z --group-by agent_type_version
```

These read `/projects/{id}/task-performance` and its `/summary` route through
the daemon. Windows select attempt **admission** time, inclusive `from` and
exclusive `to`, at most 366 days. Outcomes and session-wide usage are observed at
read time; these are not billing-event windows. Evidence pages accept the exact
returned `--cursor`. Summaries cover at most 1000 attempts in one database
snapshot; `PERFORMANCE_WINDOW_TOO_LARGE` requires a narrower window. No partial
summary is returned. Empty cohorts retain zero sample counts.

Group by `agent_type`, `agent_type_version`, `skill`, `skill_version`, `harness`,
`model`, `category` or `capability`. Overall totals include unseeded reservations
and mixed configurations. Configuration groups exclude both, reporting separate
counts; task category/capability groups retain them. Skills and capabilities may
overlap, so group counts must not be summed into a project total. These are
observational comparisons; task difficulty, attached Skills and model differences
remain possible confounders.

`assessedPassed` is historical evaluation evidence, distinct from the current
completion proof in `ao task show`. First-pass credit requires attempt one,
result one, and passing first/latest assessments. Retry counts count extra
attempts once. CI failures count attempts with failed frozen criteria; review
changes count witnessed native passes requesting changes, not individual prose
findings. Duration sums and samples include only closed reservations; ongoing
reservations are separate. Reservation time is not CPU time.

Unknown per-attempt tokens remain null; known zero remains zero. Aggregate token
sums include only their reported known samples. Native, estimated and unknown
event counts and incomplete-attempt counts remain visible. Priced cost is the
priced portion in nanos with its event count, not an observed invoice or an
estimate for unpriced events. Missing native model names remain unspecified.

## Manual smoke test

```bash
cd backend
go build -o /tmp/ao ./cmd/ao

tmp=$(mktemp -d)
export AO_RUN_FILE="$tmp/running.json"
export AO_DATA_DIR="$tmp/data"
export AO_PORT=3037

/tmp/ao status --json
/tmp/ao doctor
/tmp/ao start
/tmp/ao status --json
/tmp/ao stop
/tmp/ao status --json
rm -rf "$tmp"
```

## Adding new commands

Add a product command only when a daemon HTTP route owns the corresponding
mutation/read; the CLI must call that route rather than reimplementing daemon
behavior. Commands not yet exposed but with backend routes in place include
`ao events ...` (over the CDC/SSE endpoint) and CLI parity for PR/review
actions.

Do not port old in-process TypeScript CLI behavior that mixed command handling
with storage and runtime implementation details.

### Claiming workspace PRs

Workspace projects can claim a PR/MR on their root origin or any registered
child repository origin. Use the child's full PR/MR URL: numbers still resolve
against the root's canonical repository or origin, and a root without a remote
cannot resolve numbers. Check registered children with `ao project get <id> --json`.
Unregistered repositories are rejected even if a checkout has an additional Git
remote for them. `canonicalRepoURL` requires a valid root origin; it is not a
workspace child allowlist. Scratch projects cannot claim PRs.

For automatic attribution, workspace sessions recorded on a bare branch such as
`ao/ws-1` or `ao/ws-1-2` can use hyphen siblings (`ao/ws-1-fix` or
`ao/ws-1-2-fix`) in registered repositories. Keep the entire recorded branch,
including collision suffixes. Exact and stacked branches and `/root` slash
siblings remain supported. Matching prefers the most specific owner and leaves
ambiguous ownership for explicit claiming. Custom branches and single-repository
projects do not gain hyphen-sibling ownership.

### Claiming upstream PRs from a fork

The registered origin remains the checkout and push repository. An optional
`canonicalRepoURL` in project config explicitly authorizes one upstream repository
for PR claims. Git remotes, including a remote named `upstream`, never grant claim
permission automatically. Both identities must have the same provider and host;
claims match the entire namespace and repository, including GitLab subgroups.

For a project with no other config:

```bash
ao project set-config my-project \
  --canonical-repo-url https://github.com/my-org/my-repo
```

`set-config` replaces the whole config. For an existing configured project, read
`ao project get my-project --json`, preserve its `project.config` fields, add
`canonicalRepoURL`, and submit the complete object with `--config-json`. The same
object is accepted by `PUT /api/v1/projects/{id}/config` as `{"config": {...}}`.
Use an HTTPS repository URL, without a PR/MR suffix, credentials, query, or fragment.
Self-managed GitLab URLs and nested namespaces are supported. Explicit ports
are preserved and must match too; `gitlab.example.com:8443` is a different
authority from `gitlab.example.com`.

Both `ao session claim-pr 42` and `ao spawn --claim-pr 42` resolve numbers against
canonical when configured, otherwise origin. A full PR/MR URL may name either
identity. Unrelated repositories and different hosts/providers remain rejected.
Removing `canonicalRepoURL` restores origin-only claims. This does not unlink PRs
already claimed or move existing worktrees. Repository identity is read at claim
time, so existing sessions need no restart or duplicate project.

Migration 0126 adds an empty canonical identity to existing non-NULL config JSON
where absent, preserving all other settings and any explicit canonical value.
NULL configs retain their defaults. No Git discovery runs during migration, and
no earlier migration is modified. Downgrading preserves config data; older
versions do not support canonical claims and may drop this field when saving
project settings.
