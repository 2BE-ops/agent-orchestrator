# RUNBOOK: cut the AO Cloud "coder" provider over to EC2

End-to-end steps for a human with both AWS access (profile `ao-cloud`, account
`479575345906`, region `eu-north-1`) and Coder admin access to
`coder.aoagents.dev`. Nothing here has been executed; this is the procedure.

Prerequisites installed locally: `packer` (>= 1.9), `terraform`/`coder` CLI,
`aws` CLI configured with the `ao-cloud` profile, and a Coder API token for the
template-push (a Coder admin/template-admin token, distinct from AO's runtime
`ao-integration` token).

Do the AWS wiring in `AWS-WIRING.md` first (IAM policy on the Coder server role,
subnet, security group). You need a subnet id and a security group id before
step 2.

---

## Step 1. Build the AMI (Packer)

```bash
cd deploy/coder/ec2-template/packer
export AWS_PROFILE=ao-cloud
packer init ao-coder-ec2.pkr.hcl
packer validate ao-coder-ec2.pkr.hcl
packer build ao-coder-ec2.pkr.hcl
```

Packer launches a temporary t3.medium in `eu-north-1`, installs the exact worker
runtime (`scripts/install-worker-runtime.sh`), runs `scripts/verify.sh`, and
registers an AMI. Capture the AMI id from the final line:

```
==> Builds finished. The artifacts of successful builds are:
--> ao-coder-ec2.ao_coder: AMIs were created:
eu-north-1: ami-0abc123...
```

Record `AMI_ID=ami-0abc123...`.

To pin different harness versions, pass `-var claude_code_version=... -var
codex_version=...` etc. Defaults already match `cloud/Dockerfile`. Keep the AMI's
versions in lockstep with that Dockerfile so the EC2 runtime and the Docker
worker image never drift.

## Step 2. Push the Coder EC2 template

```bash
cd deploy/coder/ec2-template
export CODER_URL=https://coder.aoagents.dev
export CODER_SESSION_TOKEN=<coder-admin-or-template-admin-token>

coder templates push ao-linux-ec2 \
  --directory . \
  --yes
```

This creates (or versions) a template named `ao-linux-ec2` from `main.tf`. The
directory must contain only the template files (`main.tf` + `cloud-init/`);
`packer/` and the `*.md` docs are inert to Coder but you may move them out if the
push complains about extra files.

Capture the template's UUID (this is what AO pins):

```bash
coder templates versions list ao-linux-ec2          # confirm the active version
# get the template id:
curl -sS -H "Coder-Session-Token: $CODER_SESSION_TOKEN" \
  "$CODER_URL/api/v2/templates" | jq -r '.[] | select(.name=="ao-linux-ec2") | .id'
```

Record `TEMPLATE_ID=<uuid>`.

Note on parameters: AO supplies `region`, `instance_type`, `ami`, `subnet_id`,
`security_group_id` at create time from `AO_CLOUD_CODER_PARAMETERS_JSON`. The
push itself does not need them (every parameter has a default), but a manual test
build (step 3) does.

## Step 3. (Recommended) Prove the template in isolation before touching AO

Create one throwaway workspace by hand and confirm the instance boots, the agent
connects, and the durable root is a mount point.

```bash
coder create ao-ec2-smoke \
  --template ao-linux-ec2 \
  --parameter region=eu-north-1 \
  --parameter instance_type=t3.medium \
  --parameter ami=$AMI_ID \
  --parameter subnet_id=<subnet-...> \
  --parameter security_group_id=<sg-...> \
  --yes

# wait ~1 to 2 min, then:
coder ssh ao-ec2-smoke -- 'mountpoint -q /home/coder && echo DURABLE_OK; node --version; git --version; claude --version; codex --version; cursor-agent --version'
```

Expect `DURABLE_OK` and the five version lines. Then stop and start it once to
prove the durable volume survives (this is the exact contract AO's restore
depends on):

```bash
coder ssh ao-ec2-smoke -- 'echo marker-$(date +%s) | sudo tee /home/coder/.smoke'
coder stop ao-ec2-smoke --yes      # terminates the instance, keeps the EBS volume
coder start ao-ec2-smoke --yes     # fresh instance, re-attaches the volume
coder ssh ao-ec2-smoke -- 'cat /home/coder/.smoke'   # must print the same marker
coder delete ao-ec2-smoke --yes    # terminates instance AND deletes the volume
```

If the marker does not survive stop/start, do NOT proceed: AO restores will fail
closed. Re-check the durable-volume wiring in `main.tf` and the mount logic in
`cloud-init/agent-bootstrap.sh.tftpl`.

## Step 4. Point AO at the new template (config-only, no Go change)

Full detail in `CP-ENV.md`. In brief, update the Secrets Manager document
`ao-cloud/staging/coder`:

- `template_id` -> `TEMPLATE_ID` from step 2
- `parameters_json` -> `{"region":"eu-north-1","instance_type":"t3.medium","ami":"AMI_ID","subnet_id":"subnet-...","security_group_id":"sg-..."}`
- `durable_root` -> `/home/coder` (unchanged)

```bash
aws --profile ao-cloud --region eu-north-1 secretsmanager get-secret-value \
  --secret-id ao-cloud/staging/coder --query SecretString --output text > /tmp/coder.json
# edit /tmp/coder.json as above (parameters_json is a STRING of escaped JSON)
aws --profile ao-cloud --region eu-north-1 secretsmanager put-secret-value \
  --secret-id ao-cloud/staging/coder --secret-string file:///tmp/coder.json
shred -u /tmp/coder.json
```

## Step 5. Deploy the control plane (surgical, env/secret-only)

```bash
cd cloud
AWS_PROFILE=ao-cloud \
AO_CLOUD_SANDBOX_PROVIDER=coder \
CODER_SECRET_ID=ao-cloud/staging/coder \
./scripts/deploy-staging.sh "$(git rev-parse HEAD)"
```

Same CP image, fresh task-def revision that re-reads the coder secret. Wait for
the ECS service to reach steady state. (Alternative minimal path if the secret
keys are unchanged: `aws ecs update-service --cluster ao-cloud-staging --service
ao-cloud-staging-api --force-new-deployment`.)

## Step 6. Provision a real AO session and verify end to end

From the AO app (or the session API) against staging, start a session that lands
on the coder provider. Then verify:

1. A workspace appears in Coder named `ao-<hash>` owned by `ao-integration`, and
   an `aws_instance` tagged `ao.role=session-compute` appears in EC2
   (`eu-north-1`), plus its `ao.role=durable-root` EBS volume.
2. The AO worker connects: the session leaves "provisioning" and the agent/first
   task starts. This confirms the PTY bootstrap streamed ao-worker in and it
   dialed `https://staging-api.aoagents.dev`.
3. The terminal works: open the session terminal in the AO app, type a command,
   see output. This confirms the coder agent PTY path end to end on EC2.
4. Idle-pause/resume (optional but recommended): let the session idle past the
   pause threshold (or force a pause), confirm the instance terminates while the
   EBS volume remains, then interact to resume and confirm the worker reconnects
   with the SAME repository/state (durable-identity check passed).

## Step 7. Terminate / clean up the test

Delete the AO session normally (AO issues Coder delete -> instance terminates and
the durable volume is destroyed). Confirm in EC2 that both the instance and the
`ao.role=durable-root` volume for that session are gone. No orphaned volumes
should remain; if any do, they are safe to delete once their session is gone.

## Rollback

Restore the previous `template_id`
(`a209ebd5-8403-40aa-b71a-ceebe9f86352`) and `parameters_json` in
`ao-cloud/staging/coder`, and redeploy (step 5) or point the service back at the
prior task-def revision. Because provider config is stamped per session at
create time (`CoderSessionProfile`), sessions already created on the EC2 template
keep using it; only NEW sessions pick up the rollback. Drain or delete in-flight
EC2 sessions if you need a hard cutover.
