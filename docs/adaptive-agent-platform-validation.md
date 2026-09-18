# Adaptive agent platform: environment and validation evidence

Recorded 2026-09-18. The latest milestone evidence below supersedes the historical
initial audit/environment failures retained later in this file. This is not the
final platform validation report.

## Stage 05 — registry persistence (2026-09-18)

- Resumed in the user-provided full-access session. `git fetch upstream` passed;
  upstream main is `795286c4e1a58a53269f687974c820cc10561b08`. Its migrations still
  end at 0147. No upstream merge/rebase performed in this milestone.
- Added migrations 0148 (additive CDC event vocabulary) and 0149 (registry,
  immutable versions, ordered kind-safe Skill pins and durable audit), plus the
  migration ledger entries. No previously merged migration was edited.
- Agent Type/Skill content has structural limits and typed AO harness/config
  fields. Skill resources reject traversal, Windows device names, control
  characters, case-fold collisions and attempts to replace the root SKILL.md.
- Store writes validate trusted actor provenance, enforce manager modification
  and versioning policies inside the transaction, use optimistic revisions,
  retain old versions on activation/rollback, and commit audit plus trigger-owned
  CDC atomically. Activation audit retains the exact target version.

| Command (backend directory unless noted) | Final result |
| --- | --- |
| Root `npm.cmd run sqlc` | PASS, pinned sqlc v1.31.1; generated artifacts committed with sources |
| `gofmt -w` on changed Go files | PASS |
| `go test ./internal/domain ./internal/storage/sqlite/... ./internal/cdc` | PASS; final SQLite 27.671s, store 4.711s, sqlitetest 0.460s, CDC 0.489s; domain 0.533s |
| `go vet ./internal/domain ./internal/storage/sqlite/... ./internal/cdc` | PASS |
| `go build ./...` | PASS |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --path-mode=abs ./internal/domain/... ./internal/ports/... ./internal/storage/sqlite/... ./internal/cdc/...` | PASS, 0 issues |
| `go test -race ./internal/domain ./internal/storage/sqlite/... ./internal/cdc` | NOT RUN: cgo disabled; retry with `CGO_ENABLED=1` failed to compile because `gcc` is unavailable |
| `git diff --cached --check` | PASS |

Eleven new top-level tests cover invalid definitions/actors/resources, immutable
version content/hashes, exact Skill pins after Skill version activation, manager
ownership, forbidden policy escalation/promotion, twelve concurrent writers at
one revision, missing-pin rollback, injected audit failure rollback, clean DB,
populated 0147 DB upgrade with existing session/CDC preservation, database-level
history constraints, and close/reopen persistence.

Initial validation failures were fixed: new migrations needed ledger entries;
the upgrade test initially changed `latest_user_prompt`, which intentionally does
not emit session CDC, and now changes `activity_state`. Diff review added exact
activation target provenance to audit. All affected full suites were rerun.

The first full pinned lint run failed on existing Windows-only code: the
persistenthost race test uses unavailable `syscall.Kill`; process helpers report
unchecked CloseHandle, wrapped-error comparison, narrowing PID conversions and
unsafe-pointer findings. Checkout CRLF also caused goimports diagnostics; Go
working-copy line endings were normalized to LF without changing Git content.
Full-repository Windows lint remains a stage 24 gap; it is not labeled passed.

No registry API/UI, worker integration, real Electron flow, live provider call,
or complete adaptive Definition of Done has been validated at stage 05.

## Historical initial audit (superseded environment facts)

## Repository and access

| Check | Observed result |
| --- | --- |
| `git status --short` before edits | No changed files; warnings about unreadable user Git ignore file |
| `git remote -v` | `origin` fetch/push is `https://github.com/Untrivial-ai/agent-orchestrator.git` |
| `git branch --show-current` | `main` |
| `git log -1 --oneline` | `684d6d67d fix(session): bound spawn readiness and rollback (#5557)` |
| `git rev-parse HEAD` | `684d6d67db004e180f7ac9c649ce92840491ea4d` |
| `git remote rename origin upstream` | Failed: `could not lock config file .git/config`; remote unchanged |
| `git switch -c feature/adaptive-agent-platform` | Failed: cannot lock ref / create `.git/refs/heads/feature/adaptive-agent-platform` |
| `git ls-remote origin refs/heads/main` | Failed to connect to `github.com:443` |
| `gh auth status` | Reported invalid stored authentication; no credentials printed or changed |
| GitHub connector profile | Authenticated as `2BE-ops`; repository issue/PR reads succeeded |
| GitHub connector fork capability | No fork/create-repository tool exposed in available tool metadata |
| Browser fallback for fork creation | `cua.getBrowser` returned `No browser is available` |

The session permission configuration explicitly grants read-only access to
`.git`, with no approval escalation available. This prevents remotes, branch,
index, commit and worktree setup. It is not just a GitHub authentication issue.
The required architecture commit cannot precede implementation in this session.
No alternate Git directory or relocated clone was used to bypass the restriction.

Fork URL: not created. Requested branch: `feature/adaptive-agent-platform` (not
created). Starting and last incorporated upstream SHA are identical. No fetch,
merge, rebase, feature commit or push has succeeded. GitHub connector PR metadata
does not update local refs or establish the latest upstream SHA.

## Baseline commands executed

These ran before application source modifications (there have been none).

| Command / environment | Result |
| --- | --- |
| `node --version` | `v24.18.0` |
| `npm.cmd --version` | `11.16.0` |
| `Get-Command go` and inspected common install locations | Go not found on PATH; no usable compiler identified |
| `npm.cmd run typecheck` in `frontend` | Failed, exit 1; missing React/JSX, motion, clsx and tailwind-merge resolution in `packages/product-ui`, with downstream type errors |
| `npm.cmd test -- --run src/renderer/components/TaskComposer.test.tsx src/renderer/components/ProjectSettingsForm.test.tsx --maxWorkers=1` in `frontend` | Passed, exit 0: 2 files, 74 tests; Vitest reported 20.06 seconds |
| `npm.cmd ci --ignore-scripts --cache ../../.cache/adaptive-npm --fetch-retries=0 --fetch-timeout=15000` in `packages/product-ui` | Failed, exit 1; registry download returned network `EACCES`; cleanup also reported `EPERM` |

The failed dependency install may leave an incomplete ignored
`packages/product-ui/node_modules` directory. Rerun a real `npm ci` there when
network access is available; do not symlink dependencies from another checkout.
Its diagnostic log is in ignored `.cache/adaptive-npm`; it is not source output
and must not be committed. No package manifest or lockfile was intentionally
changed by the install.

## Not executed / not established

- Backend build, Go tests/race tests/vet/lint, sqlc and API generation.
- Full frontend suite, shared-package checks, desktop package and full e2e gates.
- Clean/existing database migration tests or daemon restart tests.
- Real Electron launch, screenshot inspection, or adaptive UI workflows.
- Paid model calls or authenticated live Claude/Codex/OpenCode worker runs.
- Fork creation, feature commits, branch pushes or remote CI validation.

The passing tests verify existing Task Composer and Project Settings behavior
only. They do not validate any requested new feature. The frontend typecheck
failure is baseline evidence; no claim is made that every downstream diagnostic
will disappear after dependency installation.

## Documentation review

- `git diff --check` passed for the tracked documentation change.
- A local Node check passed for relative links and trailing whitespace in all
  three new documents and confirmed all 48 Definition of Done rows are present.
- Final `git status --short` showed only `docs/README.md` and the three new
  adaptive-platform documents. Application source and manifests are unchanged.
- Final remote/branch/HEAD inspection confirmed the original `origin`, `main`
  and starting SHA remain unchanged. The documents are uncommitted.

## Required environment repair

1. Resume this checkout in a session with write access to its Git metadata.
   Preserve the draft audit/design/checklist. This is required before the
   assignment's design-commit gate and continuous-commit workflow can proceed.
2. Make a Go toolchain compatible with `backend/go.mod` (`go 1.25.7` directive)
   available, and permit dependency installation/regeneration. Use workflow
   runtime versions for final checks; Windows race checks also need a supported
   C toolchain. No system toolchain installation was attempted here.
3. Restore shell network access for Git/npm dependencies and authenticated `gh`
   access for fork/push, or provide an equivalent supported authenticated fork
   capability. Do not paste tokens into the repository or conversation.
4. Enable a supported UI inspection surface for final running-app validation.
   The browser tool currently exposes no browser and native-app APIs are disabled.

GitHub authentication alone does not block local engineering. The immediate
blocking condition is Git metadata access: it prevents the user-required commit
before major implementation. Toolchain and package access separately prevent
verified implementation/build milestones. Independent source audit and design
work was completed while those conditions were recorded.
