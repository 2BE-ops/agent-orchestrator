#!/usr/bin/env bash
# Fail the Packer build if the baked runtime does not match the worker Docker
# image contract. Run as root.
set -euxo pipefail

# Tools AO's harness tasks and the PTY bootstrap require.
for bin in bash git jq curl tar gzip sudo mountpoint lsblk install base64 mktemp pkill useradd node npm gh cursor-agent claude codex; do
  command -v "$bin" >/dev/null 2>&1 || { echo "MISSING: $bin" >&2; exit 1; }
done

# Node major must be the pinned line.
node_major="$(node --version | sed 's/^v\([0-9]*\).*/\1/')"
[ "$node_major" = "${NODE_MAJOR:-22}" ] || { echo "node major $node_major != ${NODE_MAJOR:-22}" >&2; exit 1; }

# Harness CLIs must actually run (mirrors the Dockerfile's --version smoke checks).
claude --version
codex --version
cursor-agent --version
gh --version

# Users and sudo contract.
id -u coder >/dev/null
id -u ao-worker >/dev/null
[ "$(id -u ao-worker)" = "10001" ] || { echo "ao-worker uid is not 10001" >&2; exit 1; }
sudo -n -u coder true 2>/dev/null || echo "note: passwordless sudo for coder is validated at runtime (no login session in Packer)"

echo "verify.sh passed"
