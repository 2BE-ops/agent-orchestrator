# Adaptive agent platform: environment and validation evidence

Recorded 2026-09-18. The latest milestone evidence below supersedes the historical
initial audit/environment failures retained later in this file. This is not the
final platform validation report.

## Stage 08 — native capabilities and provider references (2026-09-18)

Added adapter-declared configuration fields, bounded Chat capability inspection,
native model selection, permission controls and shared native sign-in navigation.
Provider bindings contain only a native provider reference, optional project
scope and descriptive state. Migration 0150 preserves immutable targets and
audited revision-checked label/availability changes. Credentials and arbitrary
endpoint configuration are not accepted. Required missing bindings never fall
back to defaults. All mutation provenance comes from the human controller.

The shared configuration check distinguishes invalid options from unavailable
native prerequisites. Fresh worker launch will resolve project defaults and
one-off overrides before this check in stage 09; this milestone exposes advisory
authoring checks. Unknown Skill tool/MCP requirements remain explicitly unverified
and cannot silently grant capabilities. Active-worker adoption is not a fresh
launch and must retain its snapshot in stage 09.

| Command/check | Result |
| --- | --- |
| `npm run sqlc`, `npm run api` | PASS; migration/query and code-first API artifacts regenerated |
| `go test ./internal/service/registry ./internal/domain ./internal/storage/sqlite/... ./internal/adapters/agent/codex ./internal/httpd/apispec/...` | PASS; full listed suites, SQLite 31.71s, store 8.75s |
| Configuration/registry/provider/migration/spec focused tests across service/agent, HTTP and SQLite | PASS; strict credential-field rejection, API error envelopes, disabled/missing binding, native scope/model/effort/capability validation and audit/CDC covered |
| Four focused frontend files: binding editor/checks, configuration editor, registry and API client | PASS, 46 tests, 7.64s |
| `npm run frontend:typecheck` | PASS |
| `go build ./...`; touched service/SQLite/HTTP `go vet` | PASS |
| Pinned golangci-lint over touched packages | 12 existing findings in unchanged `codex_secure_fs_windows.go`; exact categories 5 errcheck, 2 gocritic, 3 gosec, 2 staticcheck |
| Same pinned lint with `--new-from-rev=ac7e7cc26` | PASS, 0 new issues |
| `npx vite build --config vite.renderer.config.ts` | PASS, 2.53s; existing chunk-size warning. Initial command used nonexistent `.mts` suffix, corrected and rerun |
| Real isolated Electron + rebuilt Go daemon | PASS for binding/editor/validation flow below |

Earlier stage-08 full service/agent tests exposed existing Windows ACL ancestor,
symlink-privilege and model-cache timing failures. Full service/chat exposed a
duplicate conversation-message ID in the existing completed-Codex handoff test.
These are not labeled passed; they join the recorded platform/CI follow-ups.
Logs are retained under ignored `.cache/adaptive-tests/*stage08*`.

The existing isolated worktree/data were reused without dependency reinstallation.
The Go daemon was rebuilt and Electron restarted on 3036 with CDP 9336. The real
UI created `Lab native Codex`, saved and activated Agent Type v3 using the binding,
disabled that binding and observed validation rejection, re-enabled it and
observed native installed/authorized readiness, then reloaded and verified the
version persisted. The initial script expected unavailable native auth; actual
fresh readiness was ready, so verification was corrected to test the observed
state plus an explicitly disabled binding. No worker was launched. Screenshots
`electron-provider-binding.png`, `electron-provider-unavailable.png` and
`electron-provider-readiness.png` were captured and visually inspected. Custom
native provider/scope cases have injected-catalog coverage; live custom-provider
worker execution remains a later validation requirement.

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

## Stage 06 — registry API, CLI and desktop authoring (2026-09-18)

The daemon now exposes the shared registry service through 20 human authoring
routes and HTTP-only `ao agent-type` / `ao skill` commands. Actor provenance is
established by the controller; JSON role/origin spoofing is rejected. Versions
are appended inactive and activation/rollback requires the current revision.
Manager-created records use the same service and appear in the same API/UI.

| Command / check | Result |
| --- | --- |
| Registry controller tests | PASS, 4 top-level tests including full authoring lifecycle, strict input, manager policy and unavailable service |
| `go test ./internal/cli ./internal/telemetrymeta` | PASS, 13.165s / 0.015s; includes new commands, usage errors and preserved daemon error envelopes |
| Full domain, ports, SQLite, CDC, API/spec/specgen/envelope and skillassets suites | PASS |
| Full HTTP controllers suite | FAIL on two unrelated Windows tests: concurrent mobile.json rename access denied, and project clone file-URL validation; registry tests pass |
| Pinned golangci-lint v2.12.2 on domain/ports/storage/CDC/registry service/HTTP/CLI/telemetry packages | PASS, 0 issues |
| API generation | PASS; specgen then pinned openapi-typescript 7.4.4. Root dependencies restored for the normal `npm run api` command |
| Canonical generated OpenAPI comparison | 14 new path groups, 19 new schemas, zero semantic changes to existing paths/schemas; generated ordering accounts for the larger textual diff |
| `npm.cmd run frontend:typecheck` | PASS |
| Focused registry/event transport/API client/sidebar tests | PASS, 174 tests; registry rerun after accessible label fixes: 4 PASS |
| ShellTopbar tests after registry title fix | PASS, 40 tests |
| `npx vite build --config vite.renderer.config.ts` | PASS, 2.76s; route tree generated |
| Full `npm test -- --maxWorkers=2` in frontend | FAIL: 302 files passed, 22 failed; 4,718 tests passed, 169 failed, 8 skipped. Failures include macOS path/signing/update assumptions, Windows file/socket behavior, missing native SQLite binding and landing dependencies. Exact diagnostic log retained locally; stage 24 must resolve or verify in the supported CI environment |
| `git diff --cached --check` | PASS |

Actual Electron validation used the `ao-desktop-dev` skill, detached isolated
worktree `.cache/ao-adaptive-lab`, real npm installs in each workspace, and
scratch data/profile/runfile under `C:\Users\bill\.ao\dev\adaptive-platform-20260918`.
The real Go daemon listened on loopback port 3036; the actual Electron renderer
was inspected through its local CDP port 9336. No mock API, real user data or paid
worker was used. Verified through desktop controls: create Skill, create Codex
Agent Type, attach exact Skill v1, append inactive v2, compare, activate v2,
roll back to v1, inspect audit, clone, disable and retain state on renderer reload.
No renderer errors were observed in the completed interaction pass.

Screenshots inspected: `.cache/adaptive-tests/electron-registry-editor.png`,
`electron-registry-history.png`, `electron-registry-persisted.png`. UI checks
found and fixed the incorrect Board topbar title and unstable accessible names
on populated form fields. These are ignored local evidence, not committed app
state. Full daemon/desktop restart with active workers remains stage 23/25 work.
Native provider/model capability controls, portable bundles, Skill resource
materialization and worker launch are intentionally the next stages.

## Stage 07 — Skill resources and portable definitions (2026-09-18)

- Added Skill resource/tool/MCP requirement editing and inspection, portable
  schema v1 export/import for both registry kinds, CLI commands and four API
  routes. Export embeds exact pinned Skill content while excluding local IDs,
  actors and provider/account references. Missing local bindings remain explicit.
- Imports use one atomic creation transaction for up to 32 Skill dependencies
  plus the root, with disabled state and every manager permission false. An
  invalid later dependency rolls back earlier identities, versions, audit and
  CDC. Unknown JSON fields, schemas, unsafe resource paths and oversized content
  are rejected. File/directory collisions, including the generated SKILL.md
  root, are now rejected before materialization.
- Native Skill materialization and temporary attachments will consume these
  immutable contents through stage 09 worker snapshots; authoring/import does
  not write to provider Skill directories or execute resources.

| Command / check | Result |
| --- | --- |
| Focused registry domain/store/controller/CLI tests | PASS; includes portable round trip, exact historical Skill after newer activation, binding-reference exclusion, strict malformed bundle rejection and atomic rollback |
| Full domain / SQLite / store / API / spec / envelope / skillassets / CLI / telemetry suites | PASS; final domain 1.011s, SQLite 46.409s, store 18.421s, API 0.874s, CLI 20.748s |
| Full HTTP controllers suite | Same two stage 06 Windows failures (mobile config rename and file-URL clone); registry tests pass |
| Pinned golangci-lint v2.12.2 on touched packages | PASS, 0 issues |
| `go build ./...` | PASS |
| Root `npm.cmd run api` | PASS, pinned generator; spec drift tests pass |
| Root `npm.cmd run frontend:typecheck` | PASS |
| Registry/editor/transfer/API client Vitest suites | PASS, 3 files / 44 tests, 6.43s |
| Renderer Vite production build | PASS, 5.22s; existing large-chunk advisory |
| `git diff --cached --check` | PASS |

The isolated Electron and daemon were fully stopped, rebuilt and restarted
against the same scratch profile. The disabled cloned Agent Type from stage 06
was still present. Through actual desktop controls: authored a Skill resource
and tool requirement, created/activated Skill v2, exported that exact version,
downloaded JSON under the scratch AO directory, selected the file for import,
inspected requirements, imported it, and verified resource content plus disabled
state and all three manager permissions off. No renderer errors observed.
Screenshots inspected: `.cache/adaptive-tests/electron-registry-import.png` and
`electron-registry-import-policy.png`. The first download automation attempt was
cancelled by the CDP download behavior; using the page's explicit scratch
download directory verified the actual Download JSON control successfully.
No worker or paid harness call was started.

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
