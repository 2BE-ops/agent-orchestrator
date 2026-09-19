---
name: using-ao
description: "Catalog of the AO (Agent Orchestrator) `ao` CLI: spawning workers, managing sessions and projects, sending messages, controlling the shared browser, previewing pages, and daemon control. Use when using the ao CLI, spawning workers, or managing AO sessions in an AO workspace."
trigger: "Using the ao CLI in an AO workspace: spawning workers, managing sessions/projects, sending messages, controlling or previewing pages."
---

# AO CLI Catalog

`ao` is a thin CLI over the local AO daemon. Every command is `ao <command> --help` for the authoritative flag list.

| Command | What it does | When to use | Details |
|---|---|---|---|
| `spawn` | Spawn a project worker or projectless standalone worker | Starting a new task or issue | [commands/spawn.md](commands/spawn.md) |
| `session` | Manage agent sessions (list, kill, rename, restore, etc.) | Inspecting or controlling running/terminated sessions | [commands/session.md](commands/session.md) |
| `project` | Register, inspect, configure, or remove projects | Setting up or managing repos AO knows about | [commands/project.md](commands/project.md) |
| `project` controls | Pause, resume, drain, stop, cancel work, list Needs Human, dry-run plans | Steering a project deterministically without touching unrelated projects | [commands/project-control.md](commands/project-control.md) |
| `agent-type` | Author reusable, versioned worker configurations | Inspecting, creating, cloning or activating Agent Types | [commands/registry.md](commands/registry.md) |
| `skill` | Author and version composable Skills | Managing pinned reusable instructions | [commands/registry.md](commands/registry.md) |
| `task` | Inspect persistent work, context, results and typed messages | Executing a reserved task or observing its history | [commands/task.md](commands/task.md) |
| `agent-manager` | Inspect Manager governance, controllers, candidate compatibility, routing inputs, delivery and audit | Exact governance/history; human-directed configuration, native start and routing intent | [commands/agent-manager.md](commands/agent-manager.md) |
| `knowledge` | Inspect versioned project knowledge | Finding accepted project facts and their provenance | [commands/task.md](commands/task.md) |
| `orchestrator` | List orchestrator sessions | Viewing which sessions are orchestrators | [commands/orchestrator.md](commands/orchestrator.md) |
| `evolution` | Run controlled version experiments and inspect recommendations | Comparing pinned Agent Type/Skill versions with comparable cohorts | [commands/evolution.md](commands/evolution.md) |
| `review` | Submit a reviewer result for a worker's PR | Completing a code review loop | [commands/review.md](commands/review.md) |
| `send` | Send a message to a running agent session | Correcting or directing a live agent | [commands/send.md](commands/send.md) |
| `preview` | Start a session-owned app or open an exact URL/file | Running and showing the worker's relevant app, Markdown, HTML, PDF, or image | [commands/preview.md](commands/preview.md) |
| `browser` | Inspect and control the session's shared live browser | Verifying a web app through snapshots, interactions, waits, screenshots, console, and errors | [commands/browser.md](commands/browser.md) |
| `start` | Fetch (if needed) and open the AO desktop app | Launching the app | [commands/start.md](commands/start.md) |
| `stop` | Stop the AO daemon | Shutting down AO | [commands/stop.md](commands/stop.md) |
| `status` | Show daemon status | Verifying the daemon is up and healthy | [commands/status.md](commands/status.md) |
| `doctor` | Run local health checks | Diagnosing AO setup problems | [commands/doctor.md](commands/doctor.md) |
| `import` | Import projects from a legacy AO install | Migrating from the old flat-file store | [commands/import.md](commands/import.md) |
| `version` | Print version information | Checking installed version | - |
| `completion` | Generate shell completion scripts | Setting up tab completion | - |

## Conventions

- Most read commands accept `--json` for machine-readable output.
- `-p / --project` scopes session subcommand lookups to one project.
- Session and project ids are shown by `ao session ls` and `ao project ls`.
- `--agent` is an alias for `--harness` on `ao spawn`.
- Every command accepts `-h / --help` for the full flag list.
- For frontend launch, preview selection, or artifact handoff, read
  [commands/preview.md](commands/preview.md) before acting. Its static-file,
  project-runtime, and automatic-handoff rules are load-bearing.
- For page inspection, interaction, or request diagnosis, read
  [commands/browser.md](commands/browser.md). It defines shared-tab behavior
  and the opt-in network policy.

Use [references.md](references.md) only when a natural-language request does
not map clearly to a command above.
