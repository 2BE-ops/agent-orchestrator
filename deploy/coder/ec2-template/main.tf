# AO Cloud "coder" sandbox provider, EC2 backing.
#
# This Coder template provisions ONE t3.medium EC2 instance per AO session, in
# place of the shared-box "ao-linux-docker" template that provisions a
# docker_container. It is a drop-in for AO's compute-agnostic Coder provider
# (cloud/internal/sandbox/coder/client.go): AO creates one workspace per session,
# waits for the coder_agent to report connected+ready, then streams ao-worker in
# over the agent PTY (BootstrapWorker). Nothing in AO changes; only the template
# behind AO_CLOUD_CODER_TEMPLATE_ID and the parameters/durable-root config move.
#
# Lifecycle parity with the Docker template (verified against the AO reconciler
# and cloud/docs/coder-sandbox-provider.md):
#
#   * The aws_instance is gated by data.coder_workspace.me.start_count, exactly
#     like the Docker template gates its docker_container. So Coder "start"
#     (AO create / AO restore) LAUNCHES a fresh instance and Coder "stop"
#     (AO idle-pause) TERMINATES it. We never EC2 stop/start the same box, so
#     "launch on create/restore, terminate on delete, NOT stop/start" holds at
#     the instance level. Boot is ~1 to 2 minutes, which AO tolerates.
#
#   * A persistent aws_ebs_volume, NOT gated by start_count, holds AO's durable
#     state and is mounted at the durable root (AO_CLOUD_CODER_DURABLE_ROOT).
#     This is the EC2 equivalent of the Docker template's persistent
#     docker_volume: it survives Coder stop then start and is destroyed only on
#     workspace delete. AO's reconciler sets RequireDurableIdentity=true on every
#     Coder restore (reconciler.go workerBootstrap), so bootstrap FAILS CLOSED if
#     the durable-session-id marker is missing after a resume. The persistent
#     volume is what makes that marker, the checked-out repository, and the Claude
#     and Codex state survive an idle-pause and resume. Without it, every resume
#     would fail with "Coder durable state did not survive workspace stop/start".
#
# ASSUMPTION (the current Docker template is on the Coder server, not in this
# repo, so the agent shape below is reconstructed from client.go, provisioning.go
# and cloud/docs/coder-sandbox-provider.md): the workspace user is "coder" with
# passwordless sudo, the durable root is a real mount point, and the coder_agent
# name matches AO_CLOUD_CODER_AGENT_NAME (see the note on coder_agent "main").

terraform {
  required_providers {
    coder = {
      source = "coder/coder"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# ---------------------------------------------------------------------------
# Coder data sources
# ---------------------------------------------------------------------------

data "coder_workspace" "me" {}

data "coder_workspace_owner" "me" {}

# ---------------------------------------------------------------------------
# Rich parameters. AO passes these through AO_CLOUD_CODER_PARAMETERS_JSON, which
# the control plane forwards verbatim as Coder rich_parameter_values on create
# (coder/client.go Create). Every parameter carries a default so a manual
# `coder templates push` or a template-only test build still plans; AO always
# supplies the four infra values explicitly.
# ---------------------------------------------------------------------------

data "coder_parameter" "region" {
  name         = "region"
  display_name = "AWS region"
  description  = "Region the session EC2 instance and its durable volume live in."
  type         = "string"
  default      = "eu-north-1"
  mutable      = false
}

data "coder_parameter" "instance_type" {
  name         = "instance_type"
  display_name = "EC2 instance type"
  description  = "Compute size for the session. Locked decision: t3.medium."
  type         = "string"
  default      = "t3.medium"
  mutable      = false
}

data "coder_parameter" "ami" {
  name         = "ami"
  display_name = "Baked AO worker AMI"
  description  = "AMI ID produced by packer/ao-coder-ec2.pkr.hcl. Reproduces the worker Docker image package set."
  type         = "string"
  default      = "" # AO always supplies this; empty default only keeps a bare push planning.
  mutable      = false
}

data "coder_parameter" "subnet_id" {
  name         = "subnet_id"
  display_name = "Subnet ID"
  description  = "Subnet with outbound internet (public IP or NAT) for the session instance."
  type         = "string"
  # The durable volume (not gated on start_count, so it exists even while the
  # workspace is stopped) reads this subnet's AZ, so the value must resolve to a
  # single subnet even during a bare template import. Default to the Coder
  # server's own subnet (AWS-WIRING.md's recommendation); AO always overrides it
  # per session via AO_CLOUD_CODER_PARAMETERS_JSON.
  default  = "subnet-00b8c1f935f0629e0"
  mutable  = false
}

data "coder_parameter" "security_group_id" {
  name         = "security_group_id"
  display_name = "Security group ID"
  description  = "Security group allowing egress to the AO API and the Coder server. No ingress required."
  type         = "string"
  default      = ""
  mutable      = false
}

# root_volume_gb and durable_volume_gb are template-owned sizing knobs, not part
# of the AO parameter contract. AO does not send them; they take their defaults.
data "coder_parameter" "root_volume_gb" {
  name         = "root_volume_gb"
  display_name = "Root EBS volume (GiB)"
  description  = "Ephemeral OS disk. Holds the baked runtime. Destroyed with the instance."
  type         = "number"
  default      = "30"
  mutable      = false
}

data "coder_parameter" "durable_volume_gb" {
  name         = "durable_volume_gb"
  display_name = "Durable EBS volume (GiB)"
  description  = "Persistent per-session disk mounted at the durable root. Survives idle-pause, destroyed on delete."
  type         = "number"
  default      = "20"
  mutable      = false
}

# ---------------------------------------------------------------------------
# Locals
# ---------------------------------------------------------------------------

locals {
  # Must equal AO_CLOUD_CODER_DURABLE_ROOT. The Docker template uses /home/coder;
  # keep it identical here so the switch is config-for-config. See RUNBOOK.md and
  # CP-ENV.md. AO derives repository, .ao/worker, .ao/home, .ao/home/.claude,
  # .ao/home/.codex and .ao/durable-session-id beneath this path
  # (sandbox/provisioning.go NewCoderWorkspaceLayout).
  durable_root = "/home/coder"

  # Deterministic per-session workspace name AO computes
  # (coder/client.go WorkspaceName). Handy in tags for operators.
  workspace_name = data.coder_workspace.me.name
}

# ---------------------------------------------------------------------------
# AWS provider, region comes from the rich parameter (evaluated at plan time; a
# provider block may depend on a data source but not on a resource).
# ---------------------------------------------------------------------------

provider "aws" {
  region = data.coder_parameter.region.value
}

# The durable EBS volume must be created in the same AZ as the instance's subnet.
data "aws_subnet" "session" {
  id = data.coder_parameter.subnet_id.value
}

# ---------------------------------------------------------------------------
# Coder agent
#
# One agent named "main". It must match AO_CLOUD_CODER_AGENT_NAME. The AO client
# selects the agent by that name and only bootstraps once the agent is
# connected + healthy + lifecycle_state=ready (coder/client.go selectAgent,
# toEnvironment). If AO_CLOUD_CODER_AGENT_NAME is empty, AO selects the single
# agent regardless of name, so "main" is still safe.
#
# The agent binary itself is NOT baked into the AMI. init_script downloads the
# coder agent from the Coder server at boot, exactly as the Docker template's
# container does at container start, so the agent always version-matches the
# server. The AMI bakes only its runtime prerequisites (curl, CA certs, bash).
# ---------------------------------------------------------------------------

resource "coder_agent" "main" {
  arch = "amd64" # t3.medium is x86_64. For a t4g (arm64) variant, switch this and the AMI arch together.
  os   = "linux"
  dir  = local.durable_root

  # AO streams and launches ao-worker itself over the PTY. The agent's own
  # startup_script only needs to make the interactive workspace pleasant and
  # confirm the durable root is present. It must stay short: AO waits for
  # lifecycle_state=ready before bootstrapping, and a blocking startup_script
  # delays every session's worker launch.
  startup_script_behavior = "blocking"
  startup_script          = <<-EOT
    set -eu
    # The durable root is mounted by cloud-init (user_data) before this agent
    # starts. Assert it so a mount regression fails fast and visibly instead of
    # AO's bootstrap later reporting "durable root is not a mounted directory".
    if ! mountpoint -q ${local.durable_root}; then
      echo "FATAL: ${local.durable_root} is not a mount point; durable EBS volume did not attach" >&2
      exit 1
    fi
    echo "durable root ${local.durable_root} is mounted; ready for AO worker bootstrap"
  EOT

  # Environment for the interactive workspace shell. AO's worker gets its own
  # environment via the streamed worker.env (HOME, AO_WORKSPACE_DIR, CLAUDE_CONFIG_DIR,
  # CODEX_HOME, ...), so nothing AO-critical is required here. These mirror what a
  # Docker template would expose to a shell landing in the workspace.
  env = {
    AO_DURABLE_ROOT = local.durable_root
  }

  metadata {
    display_name = "CPU usage"
    key          = "cpu"
    script       = "coder stat cpu"
    interval     = 10
    timeout      = 1
  }
  metadata {
    display_name = "RAM usage"
    key          = "mem"
    script       = "coder stat mem"
    interval     = 10
    timeout      = 1
  }
  metadata {
    display_name = "Durable disk"
    key          = "durable_disk"
    script       = "coder stat disk --path ${local.durable_root}"
    interval     = 60
    timeout      = 1
  }
}

# ---------------------------------------------------------------------------
# Persistent per-session durable volume (the docker_volume equivalent).
# NOT gated by start_count, so it survives Coder stop/start (AO idle-pause and
# resume) and is destroyed only when the workspace is deleted.
# ---------------------------------------------------------------------------

resource "aws_ebs_volume" "durable" {
  availability_zone = data.aws_subnet.session.availability_zone
  size              = tonumber(data.coder_parameter.durable_volume_gb.value)
  type              = "gp3"
  encrypted         = true

  tags = {
    Name                = "ao-${local.workspace_name}-durable"
    "ao.session"        = local.workspace_name
    "ao.owner"          = data.coder_workspace_owner.me.name
    "ao.managed"        = "true"
    "ao.role"           = "durable-root"
    "Coder_Provisioned" = "true"
  }

  # Never let a template edit accidentally recreate the durable volume in place;
  # that would silently wipe a live session's state. Volume geometry changes
  # should be made through a new template version and a fresh session.
  lifecycle {
    prevent_destroy = false # set true in production once volume size is settled
    ignore_changes  = [availability_zone]
  }
}

# ---------------------------------------------------------------------------
# The session instance. Created on Coder start, terminated on Coder stop/delete.
# ---------------------------------------------------------------------------

resource "aws_instance" "workspace" {
  count = data.coder_workspace.me.start_count

  ami                         = data.coder_parameter.ami.value
  instance_type               = data.coder_parameter.instance_type.value
  subnet_id                   = data.coder_parameter.subnet_id.value
  vpc_security_group_ids      = [data.coder_parameter.security_group_id.value]
  associate_public_ip_address = true # flip to false when the subnet has a NAT gateway; see AWS-WIRING.md

  # No instance profile by default: the worker and the coder agent both dial OUT
  # and need no AWS API access. Attach one (and grant iam:PassRole to the Coder
  # role) only if you add SSM Session Manager or CloudWatch agent. See AWS-WIRING.md.
  # iam_instance_profile = "ao-coder-session"

  user_data = templatefile("${path.module}/cloud-init/agent-bootstrap.sh.tftpl", {
    durable_root   = local.durable_root
    data_volume_id = aws_ebs_volume.durable.id
    agent_token    = coder_agent.main.token
    agent_init     = coder_agent.main.init_script
  })
  # Re-run user_data if the agent token rotates on a rebuild. Each start is a new
  # instance anyway, so this is belt and suspenders.
  user_data_replace_on_change = false

  root_block_device {
    volume_size           = tonumber(data.coder_parameter.root_volume_gb.value)
    volume_type           = "gp3"
    delete_on_termination = true
    encrypted             = true
  }

  # On-demand by default (predictable ~1 to 2 min boots). To cut cost you MAY use
  # a Spot request here, but a Spot reclamation terminates the instance the same
  # way a delete does; AO would treat that as a lost sandbox and recreate on the
  # next reconcile. Durable state survives on the separate EBS volume, so the
  # recreate re-attaches it and restores the session. Enable Spot only after
  # validating that recreate path under load.
  # instance_market_options {
  #   market_type = "spot"
  #   spot_options { spot_instance_type = "one-time" }
  # }

  tags = {
    Name                = "ao-${local.workspace_name}"
    "ao.session"        = local.workspace_name
    "ao.owner"          = data.coder_workspace_owner.me.name
    "ao.managed"        = "true"
    "ao.role"           = "session-compute"
    "Coder_Provisioned" = "true"
  }

  lifecycle {
    ignore_changes = [ami] # a new AMI applies to the NEXT launch, never re-images a live box
  }
}

resource "aws_volume_attachment" "durable" {
  count = data.coder_workspace.me.start_count

  device_name  = "/dev/sdh" # Nitro remaps to an NVMe device; cloud-init finds it by volume-id serial
  volume_id    = aws_ebs_volume.durable.id
  instance_id  = aws_instance.workspace[0].id
  force_detach = true # let Coder stop terminate the instance without a stuck detach
}

# ---------------------------------------------------------------------------
# coder_metadata: surface the compute shape in the Coder UI (operator-facing).
# ---------------------------------------------------------------------------

resource "coder_metadata" "workspace" {
  count       = data.coder_workspace.me.start_count
  resource_id = aws_instance.workspace[0].id

  item {
    key   = "instance type"
    value = data.coder_parameter.instance_type.value
  }
  item {
    key   = "region"
    value = data.coder_parameter.region.value
  }
  item {
    key   = "durable root"
    value = local.durable_root
  }
  item {
    key   = "durable volume"
    value = aws_ebs_volume.durable.id
  }
}
