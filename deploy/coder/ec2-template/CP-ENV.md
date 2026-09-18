# Control-plane env change for the EC2 template

## The change is config-only. No Go change is required.

The AO control plane's Coder provider is compute-agnostic. It references a
template by UUID and forwards rich parameters verbatim
(`cloud/internal/sandbox/coder/client.go` `Create`); it never knows or cares
whether the template makes a `docker_container` or an `aws_instance`. Switching
to EC2 is therefore three values:

| Env var | Old (Docker template) | New (EC2 template) |
| --- | --- | --- |
| `AO_CLOUD_CODER_TEMPLATE_ID` | `a209ebd5-8403-40aa-b71a-ceebe9f86352` (ao-linux-docker) | the UUID printed by `coder templates push` for the EC2 template |
| `AO_CLOUD_CODER_PARAMETERS_JSON` | e.g. `{}` or docker params | `{"region":"eu-north-1","instance_type":"t3.medium","ami":"ami-XXXXXXXX","subnet_id":"subnet-XXXXXXXX","security_group_id":"sg-XXXXXXXX"}` |
| `AO_CLOUD_CODER_DURABLE_ROOT` | `/home/coder` | `/home/coder` (unchanged; the EC2 template mounts the durable EBS volume there) |

Everything else (`AO_CLOUD_CODER_URL`, `_TOKEN`, `_OWNER`, `_AGENT_NAME`,
`_WORKER_TOKEN_TTL`, `AO_CLOUD_SANDBOX_PROVIDER=coder`) stays as is.

`AO_CLOUD_CODER_AGENT_NAME`: leave it as whatever the deployment already uses.
The EC2 template's agent is named `main`. If `AO_CLOUD_CODER_AGENT_NAME` is set,
it must equal `main` (or rename the `coder_agent` resource in `main.tf` to
match). If it is empty, AO selects the single agent by position and `main` is
fine.

## Why no Go change (the one thing that could have forced one)

AO's reconciler sets `RequireDurableIdentity=true` on every Coder restore
(`reconciler.go` `workerBootstrap`, because `record.Provider == ProviderCoder &&
(restoring || WorkerLastSeenAt != nil)`). On resume, bootstrap re-reads
`<durable_root>/.ao/durable-session-id` and FAILS CLOSED if the marker is gone.
A naive "fresh instance every time with no persistence" design would trip this on
the first idle-pause/resume with "Coder durable state did not survive workspace
stop/start".

The EC2 template avoids that entirely by carrying a persistent `aws_ebs_volume`
(not gated by `start_count`) mounted at the durable root, exactly the way the
Docker template carries a persistent `docker_volume`. The marker, the checked-out
repository, and the Claude/Codex state live on that volume and survive Coder
stop/start; the volume is destroyed only on workspace delete. So the CP contract
is satisfied unchanged. This is the crux of the design; see `main.tf` header.

## How the values reach the running task

The staging deploy stores these under the Secrets Manager JSON document
`ao-cloud/staging/coder`, and the task-definition renderer
(`cloud/scripts/lib/deployment.py`, `CODER_SECRET_ENV`) maps each JSON key to a
container `secrets` entry with a `valueFrom` pointing at that key. So the CP
reads them at task start from Secrets Manager, not from plaintext env.

Update the secret's JSON, keys `template_id`, `parameters_json`, `durable_root`:

```bash
aws --profile ao-cloud --region eu-north-1 secretsmanager get-secret-value \
  --secret-id ao-cloud/staging/coder --query SecretString --output text > /tmp/coder.json

# edit /tmp/coder.json:
#   "template_id":     "<new EC2 template uuid>"
#   "parameters_json": "{\"region\":\"eu-north-1\",\"instance_type\":\"t3.medium\",\"ami\":\"ami-XXXX\",\"subnet_id\":\"subnet-XXXX\",\"security_group_id\":\"sg-XXXX\"}"
#   "durable_root":    "/home/coder"
# (parameters_json is a STRING containing JSON; escape the inner quotes)

aws --profile ao-cloud --region eu-north-1 secretsmanager put-secret-value \
  --secret-id ao-cloud/staging/coder --secret-string file:///tmp/coder.json
shred -u /tmp/coder.json  # it contains the Coder token
```

Then roll the task so it re-reads the secret. The established pattern for an
env/secret-only bump (see the `:110`-off-`:109` note in the deploy history) is to
re-run the deploy script for the SAME already-deployed release SHA, which
re-renders and registers a fresh task-def revision (same CP image digest) that
re-materializes env + secrets, then updates the service:

```bash
cd cloud
AWS_PROFILE=ao-cloud \
AO_CLOUD_SANDBOX_PROVIDER=coder \
CODER_SECRET_ID=ao-cloud/staging/coder \
./scripts/deploy-staging.sh "$(git rev-parse HEAD)"   # pass the SHA already running on staging
```

The script reads `ao-cloud/staging/coder` (via `CODER_SECRET_ID`), validates it
through `deployment.py` `_validate_coder_settings` (which will reject a malformed
`parameters_json` or an unsafe `durable_root` before anything ships), registers
the revision, and rolls the service. The working tree must be clean and the SHA
must resolve to the current commit, per the script's own guards.

If you prefer the minimal AWS-only path (the secret `valueFrom` keys are
unchanged, only the secret's contents changed), a plain force-new-deployment also
re-pulls the secret on the new tasks:

```bash
aws --profile ao-cloud --region eu-north-1 ecs update-service \
  --cluster ao-cloud-staging --service ao-cloud-staging-api --force-new-deployment
```

Registering a fresh revision is preferred because it gives a clean revision
number to roll back to. Roll back by pointing the service at the prior revision
and restoring the old `template_id`/`parameters_json` in the secret.

Note: `AO_CLOUD_NODEOPS_*` values are left in the secret document but the renderer
strips stale provider values for the non-selected provider, so they do not reach
the coder-provider task.
