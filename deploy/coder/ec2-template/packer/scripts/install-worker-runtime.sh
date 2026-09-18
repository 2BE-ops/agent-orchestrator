#!/usr/bin/env bash
# Reproduce the worker Docker image runtime (cloud/Dockerfile "worker" stage) on
# an Ubuntu 22.04 AMI. Run as root by Packer.
#
# Version pins come from env (set by the Packer build, defaulted to the current
# Dockerfile ARGs): NODE_MAJOR, CLAUDE_CODE_VERSION, CODEX_VERSION,
# CURSOR_AGENT_VERSION, GH_VERSION.
set -euxo pipefail

: "${NODE_MAJOR:=22}"
: "${CLAUDE_CODE_VERSION:=2.1.228}"
: "${CODEX_VERSION:=0.147.0}"
: "${CURSOR_AGENT_VERSION:=2026.08.11-e8db854}"
: "${GH_VERSION:=2.97.0}"

# This AMI targets t3.medium (x86_64). Keep the arch mapping explicit so a build
# on the wrong builder fails loudly instead of baking mismatched binaries.
DPKG_ARCH="$(dpkg --print-architecture)" # amd64 on x86_64
case "$DPKG_ARCH" in
  amd64) GH_ARCH=amd64; CURSOR_ARCH=x64 ;;
  arm64) GH_ARCH=arm64; CURSOR_ARCH=arm64 ;;
  *) echo "unsupported build architecture: $DPKG_ARCH" >&2; exit 1 ;;
esac

# --- 1. Base packages (mirrors the Dockerfile apt set) --------------------
# The Dockerfile installs: bash ca-certificates curl git jq openssh-client
# procps tar. On the AMI we add the OS tools AO's PTY bootstrap relies on, which
# the distroless-style container got from its base image:
#   sudo         passwordless privilege escalation for the coder user
#   util-linux   mountpoint (durable-root contract check), lsblk (cloud-init)
#   coreutils    install, base64, mktemp, id  (already present, listed for intent)
#   gzip         bootstrap archive decompression
#   passwd/adduser  useradd (create the ao-worker user)
apt-get update
apt-get upgrade --yes
apt-get install --yes --no-install-recommends \
  bash \
  ca-certificates \
  curl \
  git \
  gnupg \
  jq \
  openssh-client \
  procps \
  sudo \
  tar \
  util-linux \
  coreutils \
  gzip \
  passwd

# --- 2. Node.js (major pinned to match node:22 base image) ----------------
# NodeSource pins the major line; the container tracks node:22 the same way.
curl --fail --location --silent --show-error "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash -
apt-get install --yes --no-install-recommends nodejs

# --- 3. GitHub CLI (exact version + install method from the Dockerfile) ----
curl --fail --location --silent --show-error \
  "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${GH_ARCH}.tar.gz" \
  | tar --strip-components=2 -xzf - -C /usr/local/bin \
      "gh_${GH_VERSION}_linux_${GH_ARCH}/bin/gh"

# --- 4. Coding-agent harnesses (exact versions + methods from the Dockerfile) --
npm install --global \
  "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
  "@openai/codex@${CODEX_VERSION}"

mkdir -p "/opt/cursor-agent/${CURSOR_AGENT_VERSION}"
curl --fail --location --silent --show-error \
  "https://downloads.cursor.com/lab/${CURSOR_AGENT_VERSION}/linux/${CURSOR_ARCH}/agent-cli-package.tar.gz" \
  | tar --strip-components=1 -xzf - -C "/opt/cursor-agent/${CURSOR_AGENT_VERSION}"
ln -sf "/opt/cursor-agent/${CURSOR_AGENT_VERSION}/cursor-agent" /usr/local/bin/cursor-agent

# --- 5. Users and sudo -----------------------------------------------------
# coder: the workspace/agent user. Passwordless sudo is required by AO's
# bootstrap (cloud/docs/coder-sandbox-provider.md). Home is /home/coder, which
# the durable EBS volume mounts over at runtime.
if ! id -u coder >/dev/null 2>&1; then
  useradd --create-home --shell /bin/bash coder
fi
install -d -m 0755 /etc/sudoers.d
cat > /etc/sudoers.d/coder <<'EOF'
coder ALL=(ALL) NOPASSWD:ALL
Defaults:coder !requiretty
EOF
chmod 0440 /etc/sudoers.d/coder
visudo -cf /etc/sudoers.d/coder

# ao-worker: the unprivileged worker user, baked to match the Docker worker image
# (uid/gid 10001). AO's bootstrap detects it exists and skips useradd, then carves
# the repository and .ao directories beneath the durable root and chowns them to
# this user. The home lives under the durable root and is created at bootstrap.
if ! getent group ao-worker >/dev/null 2>&1; then
  groupadd --gid 10001 ao-worker
fi
if ! id -u ao-worker >/dev/null 2>&1; then
  useradd --uid 10001 --gid ao-worker --home-dir /home/coder/.ao/home --shell /bin/bash --no-create-home ao-worker
fi

# --- 6. Trim apt caches ----------------------------------------------------
apt-get clean
rm -rf /var/lib/apt/lists/*

echo "install-worker-runtime.sh complete"
