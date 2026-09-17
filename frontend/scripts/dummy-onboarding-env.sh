#!/usr/bin/env bash
# Launch Agent Orchestrator in a throwaway "nothing is set up yet" environment so
# first-run onboarding can be walked by hand.
#
# What it fakes
#   - The GitHub CLI and every coding-agent CLI are hidden from PATH, so the
#     GitHub prerequisite and the agent rows report as missing.
#   - GH_CONFIG_DIR points at an empty directory, so gh reads as signed out
#     whenever it is visible.
#   - AO_DEV_ELECTRON_DIR / AO_RUN_FILE / AO_DATA_DIR / AO_PORT keep the app,
#     its daemon, its Chromium profile, and its state completely separate from
#     any other running AO instance.
#
# What it does NOT fake
#   - Installing: with package managers visible, clicking Install really runs
#     brew/npm against this machine. Pass --no-installers to hide brew, npm,
#     node, bun, uv, and pipx as well, which makes install attempts fail safely
#     so the failure and retry states can be inspected without side effects.
#   - Harness sign-in: to see the installed-but-signed-out state you have to
#     install a harness for real (or already have one visible on PATH).
#
# Convergence: which states can actually move
#   - npm-installed harnesses converge. Global npm installs are redirected into
#     $AO_DUMMY_ROOT/npm-global (via NPM_CONFIG_PREFIX), that bin directory is
#     on the shim PATH, so after an install the app really does see the harness
#     and the row flips from Install to ready. Your global npm is untouched.
#   - gh does NOT converge while it is hidden: brew installs to /opt/homebrew,
#     which the shim excludes. Use --with-gh to test the GitHub states: gh is
#     visible, GH_CONFIG_DIR is isolated, so the app reads "installed, signed
#     out" and a real sign-in writes credentials to the dummy profile only.
#   - brew-installed harnesses behave like gh: they land outside the shim.
#
# Usage
#   scripts/dummy-onboarding-env.sh [--with-gh] [--no-installers] [--hide-tmux]
#                                   [--skip-build] [--dry-run]
#                                   [-- <extra electron-forge args>]
#
# State lives under $AO_DUMMY_ROOT (default /tmp/ao-dummy); delete that
# directory for a truly fresh run.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
STATE_ROOT="${AO_DUMMY_ROOT:-/tmp/ao-dummy}"
SHIM_DIR="$STATE_ROOT/bin"

hide_installers=0
hide_tmux=0
skip_build=0
with_gh=0
dry_run=0
extra_args=()

while [ $# -gt 0 ]; do
	case "$1" in
		--with-gh) with_gh=1 ;;
		--no-installers) hide_installers=1 ;;
		--hide-tmux) hide_tmux=1 ;;
		--skip-build) skip_build=1 ;;
		--dry-run) dry_run=1 ;;
		--) shift; extra_args=("$@"); break ;;
		*) extra_args+=("$1") ;;
	esac
	shift
done

# Agent CLI names across the supported harness catalog.
hidden_names="gh claude codex opencode copilot cursor-agent aider goose droid kimi amp agy crush cline qwen continue grok kilocode kimchi muse vibe auggie autohand prime-agent ampcode droidcli"
if [ "$with_gh" = "1" ]; then
	hidden_names="${hidden_names#gh }"
fi
if [ "$hide_tmux" = "1" ]; then
	hidden_names="$hidden_names tmux"
fi
if [ "$hide_installers" = "1" ]; then
	hidden_names="$hidden_names brew npm node npm-cli.js bun uv uvx pipx corepack"
fi

is_hidden() {
	local name="$1" candidate
	for candidate in $hidden_names; do
		[ "$name" = "$candidate" ] && return 0
	done
	return 1
}

mkdir -p "$SHIM_DIR" "$STATE_ROOT/electron" "$STATE_ROOT/data" "$STATE_ROOT/gh"
mkdir -p "$STATE_ROOT/npm-global/bin" "$STATE_ROOT/npm-global/lib"

# Rebuild the shim from scratch so a stale hiding list cannot linger.
find "$SHIM_DIR" -mindepth 1 -maxdepth 1 -print0 2>/dev/null | xargs -0 rm -f 2>/dev/null || true

linked=0
old_ifs="$IFS"
IFS=":"
for dir in $PATH; do
	IFS="$old_ifs"
	[ -d "$dir" ] || { IFS=":"; continue; }
	for entry in "$dir"/*; do
		[ -e "$entry" ] || continue
		name="${entry##*/}"
		is_hidden "$name" && continue
		[ -e "$SHIM_DIR/$name" ] || ln -s "$entry" "$SHIM_DIR/$name" 2>/dev/null || true
		linked=$((linked + 1))
	done
	IFS=":"
done
IFS="$old_ifs"

# A ready-to-pick project folder for the "Open local folder" step.
PROJECT_DIR="$STATE_ROOT/project"
if [ ! -d "$PROJECT_DIR/.git" ]; then
	mkdir -p "$PROJECT_DIR"
	printf '# Dummy project\n\nCreated by scripts/dummy-onboarding-env.sh for onboarding testing.\n' > "$PROJECT_DIR/README.md"
	git -C "$PROJECT_DIR" init -q
	git -C "$PROJECT_DIR" config user.email "dummy@example.com"
	git -C "$PROJECT_DIR" config user.name "AO Dummy"
	git -C "$PROJECT_DIR" add README.md
	git -C "$PROJECT_DIR" commit -q -m "chore: initialize dummy project"
fi

echo "dummy onboarding environment"
echo "  state root      $STATE_ROOT"
echo "  PATH shim       $SHIM_DIR ($linked entries)"
echo "  hidden          $hidden_names"
echo "  gh config       $STATE_ROOT/gh (empty: signed out when gh is visible)"
echo "  npm prefix      $STATE_ROOT/npm-global (npm harness installs land here)"
echo "  pick this repo  $PROJECT_DIR"
echo
if [ "$with_gh" = "1" ]; then
	echo "  expect: onboarding opens on step 1; the GitHub card offers Sign in;"
	echo "          agent rows offer Install, and npm harness installs converge."
else
	echo "  expect: onboarding opens on step 1; the project step offers Install gh;"
	echo "          agent rows offer Install (and no harness is signed in)."
fi
echo

cd "$FRONTEND_ROOT"
export PATH="$SHIM_DIR:$STATE_ROOT/npm-global/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export AO_DUMMY_ROOT="$STATE_ROOT"
export AO_DEV_ELECTRON_DIR="$STATE_ROOT/electron"
export AO_RUN_FILE="$STATE_ROOT/running.json"
export AO_DATA_DIR="$STATE_ROOT/data"
export AO_PORT="${AO_PORT:-4751}"
export GH_CONFIG_DIR="$STATE_ROOT/gh"
# Global npm installs go to the dummy prefix, which is on the shim PATH, so an
# install the app starts is one the app can then see.
export NPM_CONFIG_PREFIX="$STATE_ROOT/npm-global"

if [ "$dry_run" = "1" ]; then
	echo "dry run: environment prepared, app not launched."
	echo "  PATH=$PATH"
	echo "  AO_RUN_FILE=$AO_RUN_FILE"
	echo "  AO_DATA_DIR=$AO_DATA_DIR"
	echo "  AO_DEV_ELECTRON_DIR=$AO_DEV_ELECTRON_DIR"
	echo "  GH_CONFIG_DIR=$GH_CONFIG_DIR"
	echo "  NPM_CONFIG_PREFIX=$NPM_CONFIG_PREFIX"
	exit 0
fi

if [ "$skip_build" != "1" ]; then
	# predev builds the daemon binary and the runtime assets forge expects.
	npm run predev
fi

# electron-forge needs its own "--" before args meant for the Electron app.
if [ "${#extra_args[@]}" -gt 0 ]; then
	exec ./node_modules/.bin/electron-forge start -- "${extra_args[@]}"
fi
exec ./node_modules/.bin/electron-forge start
