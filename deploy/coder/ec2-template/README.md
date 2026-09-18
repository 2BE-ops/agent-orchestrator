# AO Cloud "coder" provider: EC2 backing

Convert the AO Cloud "coder" sandbox provider from Docker containers on a shared
box to ONE EC2 instance per session. Locked decisions: instance type
`t3.medium`, a baked AMI built from the worker Docker image's package set,
lifecycle identical to Docker today (launch on create/restore, terminate on
delete, NOT EC2 stop/start), boot ~1 to 2 minutes acceptable.

This directory is artifacts only. Nothing here builds an AMI, pushes a template,
or touches AWS/Coder. Run it via `RUNBOOK.md`.

## Files

| File | Deliverable |
| --- | --- |
| `main.tf` | Coder Terraform template: `aws_instance` (t3.medium default) + persistent `aws_ebs_volume` + `coder_agent` "main". |
| `cloud-init/agent-bootstrap.sh.tftpl` | EC2 user_data: mount the durable volume, launch the coder agent. |
| `packer/ao-coder-ec2.pkr.hcl` | AMI build (Ubuntu 22.04) reproducing the worker Docker image runtime. |
| `packer/scripts/install-worker-runtime.sh` | Exact package/harness install, derived from `cloud/Dockerfile`. |
| `packer/scripts/verify.sh` | Build-time assertion that the runtime matches the container contract. |
| `AWS-WIRING.md` | IAM policy, VPC/subnet/SG, instance-profile requirements. |
| `CP-ENV.md` | The exact control-plane env change (config-only, no Go change). |
| `RUNBOOK.md` | End-to-end execution steps. |

## How it maps to the AO control plane (which does NOT change)

AO's Coder provider is compute-agnostic (`cloud/internal/sandbox/coder/client.go`):
it creates one Coder workspace per session from a template UUID, forwards rich
parameters verbatim, waits for the named `coder_agent` to be connected + healthy
+ `ready`, then streams `ao-worker` in over the agent PTY (`BootstrapWorker`,
with `AO_WORKER_EXPECTED_SHA256` self-heal). It maps AO create/restore to Coder
start and AO delete to Coder delete; idle-pause maps to Coder stop.

So swapping the compute is swapping the template behind the UUID. This template
keeps every contract the provider and `cloud/docs/coder-sandbox-provider.md`
require:

- A Linux workspace with one `coder_agent` (named `main`).
- The workspace user `coder` has passwordless sudo (baked into the AMI).
- The durable root (`AO_CLOUD_CODER_DURABLE_ROOT` = `/home/coder`) is a real,
  non-symlink mount point (`mountpoint` is baked; cloud-init mounts the EBS
  volume there) that survives Coder stop then start.
- `git`, CA certificates, `node`, and the harness CLIs (Claude Code, Codex,
  Cursor) are baked, matching the worker Docker image.
- `ao-worker` is NOT baked; AO streams it. The coder agent binary is NOT baked;
  init_script downloads it at boot, version-matched to the server, exactly as
  the container does.

### The lifecycle, precisely

```
AO create   -> Coder start (start_count=1) -> RunInstances (fresh)  + create/attach durable EBS volume
AO restore  -> Coder start (start_count=1) -> RunInstances (fresh)  + re-attach the SAME durable EBS volume
AO idle-pause -> Coder stop (start_count=0) -> TerminateInstances   ; durable EBS volume RETAINED
AO delete   -> Coder delete                 -> TerminateInstances   ; durable EBS volume DESTROYED
```

The `aws_instance` is gated by `data.coder_workspace.me.start_count`, so it is
created on start and terminated on stop/delete, mirroring the Docker template's
`docker_container`. The `aws_ebs_volume` is NOT gated, so it persists across
stop/start (like the Docker `docker_volume`) and is destroyed only on delete.
No EC2 stop/start of the same instance ever happens.

### Durable layout beneath `/home/coder` (AO-derived, unchanged)

| State | Path |
| --- | --- |
| Repository + uncommitted files | `/home/coder/repository` |
| Worker token, git helper, logs | `/home/coder/.ao/worker` |
| Worker `HOME` | `/home/coder/.ao/home` |
| Claude config/conversations | `/home/coder/.ao/home/.claude` |
| Codex config/conversations | `/home/coder/.ao/home/.codex` |
| Restore identity marker | `/home/coder/.ao/durable-session-id` |

## Key assumptions (call these out in review)

1. **The current Docker template lives on the Coder server, not in this repo.**
   The `coder_agent` shape (name `main`, user `coder`, durable root
   `/home/coder`, blocking short startup_script, standard cpu/mem/disk metadata)
   is RECONSTRUCTED from `client.go`, `provisioning.go`, and
   `cloud/docs/coder-sandbox-provider.md`, plus the memory that the live template
   is `ao-linux-docker` (id `a209ebd5-8403-40aa-b71a-ceebe9f86352`) using
   `count = start_count` with a `docker_volume` at `/home/coder`. If the real
   template names its agent something other than `main`, either match it here or
   set `AO_CLOUD_CODER_AGENT_NAME` accordingly.

2. **A persistent durable volume is REQUIRED, and it is why no Go change is
   needed.** AO's reconciler sets `RequireDurableIdentity=true` on Coder restores,
   so a resume with an empty durable root fails closed. The persistent EBS volume
   preserves the identity marker and repository across idle-pause/resume, exactly
   like the Docker `docker_volume`. "NOT stop/start" refers to the INSTANCE
   (terminate + relaunch, never EC2 StopInstances/StartInstances); the volume
   persistence is orthogonal and matches Docker.

3. **`durable_root` stays `/home/coder`.** The provider does not assume this path
   (it is configurable), but keeping it identical to the Docker template makes
   the switch config-for-config and avoids re-teaching operators a new path. The
   EBS volume mounts over the coder user's home, so the agent's home is on the
   persistent volume, mirroring the Docker template.

4. **Ubuntu 22.04, not Amazon Linux 2023.** The worker image is Debian-based
   (`node:22-trixie-slim`); Ubuntu 22.04 is the same family + libc + apt, so the
   harnesses and glibc binaries behave identically with the least divergence, and
   it is the canonical Coder AWS base. Justification is in the Packer file header.

5. **Nitro NVMe device discovery.** t3.medium is Nitro, so `/dev/sdh` is remapped
   to an NVMe device. cloud-init finds the durable volume by matching the EBS
   volume id in the NVMe serial, with a classic-device-name fallback.

6. **On-demand, egress-only, no instance profile.** Spot is left commented (a
   reclamation looks like a delete; recreate re-attaches the volume, but validate
   first). No ingress; the agent and worker dial out. No instance profile unless
   you add SSM/CloudWatch (then `iam:PassRole` per `AWS-WIRING.md`).

7. **Harness versions are pinned to `cloud/Dockerfile` ARGs** (claude-code
   2.1.228, codex 0.147.0, cursor-agent 2026.08.11-e8db854, gh 2.97.0, node 22).
   Bump the Packer vars and the Dockerfile together so the two runtimes never
   drift.
