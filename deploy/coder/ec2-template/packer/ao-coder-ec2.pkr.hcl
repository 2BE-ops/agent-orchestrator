# Packer build for the AO Cloud "coder" EC2 session AMI.
#
# Base: Ubuntu 22.04 LTS (jammy), x86_64.
#
# Why Ubuntu 22.04 rather than Amazon Linux 2023: the worker Docker image
# (cloud/Dockerfile, "worker" stage) is Debian-based (node:22-trixie-slim, glibc,
# apt). Ubuntu 22.04 is the same Debian family and libc, so the harness CLIs, git,
# and glibc-linked binaries behave identically to the container with the least
# divergence. The harnesses install OS-agnostically (npm global + curl tarballs),
# so AL2023 would also work, but AL2023 uses dnf and a different default user and
# would drift further from the reference Dockerfile. Ubuntu 22.04 is also the
# canonical Coder AWS base image, so cloud-init and the coder agent are well
# exercised on it.
#
# The result reproduces EXACTLY the worker Docker image package/runtime set:
#   node 22, git, gh 2.97.0, jq, curl, bash, tar, procps, openssh-client,
#   ca-certificates, @anthropic-ai/claude-code@2.1.228, @openai/codex@0.147.0,
#   cursor-agent 2026.08.11-e8db854
# plus the OS tools AO's PTY bootstrap needs (sudo, useradd/shadow-utils,
# coreutils install/base64, gzip, util-linux mountpoint, procps pkill) and the
# coder + ao-worker users. It deliberately does NOT bake ao-worker or the coder
# agent binary: AO streams ao-worker at bootstrap (with AO_WORKER_EXPECTED_SHA256
# self-heal) and the coder agent is downloaded by init_script at boot, exactly as
# the container does.

packer {
  required_plugins {
    amazon = {
      source  = "github.com/hashicorp/amazon"
      version = ">= 1.2.0"
    }
  }
}

variable "region" {
  type    = string
  default = "eu-north-1"
}

variable "build_instance_type" {
  type    = string
  default = "t3.medium"
}

# Version pins, kept identical to cloud/Dockerfile ARGs. Bump these in lockstep
# with the Dockerfile so the AMI and the Docker worker image never diverge.
variable "node_major" {
  type    = string
  default = "22"
}
variable "claude_code_version" {
  type    = string
  default = "2.1.228"
}
variable "codex_version" {
  type    = string
  default = "0.147.0"
}
variable "cursor_agent_version" {
  type    = string
  default = "2026.08.11-e8db854"
}
variable "gh_version" {
  type    = string
  default = "2.97.0"
}

locals {
  ami_name = "ao-coder-ec2-{{timestamp}}"
}

# Latest Canonical Ubuntu 22.04 (jammy) amd64 hvm ebs image.
source "amazon-ebs" "ao_coder" {
  region        = var.region
  instance_type = var.build_instance_type
  ssh_username  = "ubuntu"
  ami_name      = local.ami_name
  ami_description = "AO Cloud coder EC2 session runtime (mirrors cloud/Dockerfile worker stage)"

  source_ami_filter {
    filters = {
      name                = "ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    owners      = ["099720109477"] # Canonical
    most_recent = true
  }

  launch_block_device_mappings {
    device_name           = "/dev/sda1"
    volume_size           = 30
    volume_type           = "gp3"
    delete_on_termination = true
  }

  tags = {
    Name                 = local.ami_name
    "ao.role"            = "coder-ec2-session-ami"
    "ao.managed"         = "true"
    node_major           = var.node_major
    claude_code_version  = var.claude_code_version
    codex_version        = var.codex_version
    cursor_agent_version = var.cursor_agent_version
    gh_version           = var.gh_version
  }
}

build {
  name    = "ao-coder-ec2"
  sources = ["source.amazon-ebs.ao_coder"]

  provisioner "shell" {
    environment_vars = [
      "NODE_MAJOR=${var.node_major}",
      "CLAUDE_CODE_VERSION=${var.claude_code_version}",
      "CODEX_VERSION=${var.codex_version}",
      "CURSOR_AGENT_VERSION=${var.cursor_agent_version}",
      "GH_VERSION=${var.gh_version}",
      "DEBIAN_FRONTEND=noninteractive",
    ]
    execute_command = "sudo -E bash '{{ .Path }}'"
    script          = "${path.root}/scripts/install-worker-runtime.sh"
  }

  # Fail the build if any required tool or user is missing.
  provisioner "shell" {
    execute_command = "sudo -E bash '{{ .Path }}'"
    script          = "${path.root}/scripts/verify.sh"
  }
}
