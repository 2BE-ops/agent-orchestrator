# AWS wiring for the AO Cloud coder EC2 session template

This is what a human with AWS access sets up ONCE, before the EC2 template can
launch and terminate session instances. Everything here is on the Coder side of
the boundary; the AO control plane needs no AWS access and none of this.

Environment (locked):

- Account: `479575345906`
- Region: `eu-north-1`
- AWS CLI profile: `ao-cloud`
- Coder server: EC2 `i-07d16a73a9f1e02e7` (m6i.xlarge), reached at
  `coder.aoagents.dev` via a cloudflared tunnel.

## 1. Who calls the EC2 API

The Coder deployment's Terraform (running inside the Coder server process on
`i-07d16a73a9f1e02e7`) is what plans and applies this template, so IT is the
principal that must be allowed to launch and terminate instances and manage the
durable volumes.

Give the Coder server an EC2 instance role (attached to `i-07d16a73a9f1e02e7`)
so the AWS provider in the template uses instance-role credentials with no
static keys. The template's `provider "aws"` block sets only `region`, so it
picks up that role automatically.

If you would rather not use the instance role, create an IAM user with the same
policy and put its keys in the Coder server's environment
(`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`); the instance role is preferred.

## 2. IAM policy for the Coder principal

Attach this policy to the role (or user) from section 1. It is scoped to the
region and to AO-managed resources by tag where the API allows a condition.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ReadPlanInputs",
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus",
        "ec2:DescribeInstanceAttribute",
        "ec2:DescribeInstanceTypes",
        "ec2:DescribeInstanceCreditSpecifications",
        "ec2:DescribeImages",
        "ec2:DescribeSubnets",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeVpcs",
        "ec2:DescribeVolumes",
        "ec2:DescribeVolumeAttribute",
        "ec2:DescribeVolumesModifications",
        "ec2:DescribeTags",
        "ec2:DescribeNetworkInterfaces",
        "ec2:DescribeAvailabilityZones",
        "ec2:DescribeSpotInstanceRequests"
      ],
      "Resource": "*",
      "Condition": { "StringEquals": { "aws:RequestedRegion": "eu-north-1" } }
    },
    {
      "Sid": "LaunchAndTerminateSessions",
      "Effect": "Allow",
      "Action": [
        "ec2:RunInstances",
        "ec2:TerminateInstances",
        "ec2:CreateTags"
      ],
      "Resource": "*",
      "Condition": { "StringEquals": { "aws:RequestedRegion": "eu-north-1" } }
    },
    {
      "Sid": "ManageDurableVolumes",
      "Effect": "Allow",
      "Action": [
        "ec2:CreateVolume",
        "ec2:DeleteVolume",
        "ec2:AttachVolume",
        "ec2:DetachVolume"
      ],
      "Resource": "*",
      "Condition": { "StringEquals": { "aws:RequestedRegion": "eu-north-1" } }
    }
  ]
}
```

Notes:

- `ec2:StartInstances` / `ec2:StopInstances` are deliberately NOT granted. This
  template terminates and relaunches instances (count = start_count); it never
  EC2 stop/starts the same box.
- `ec2:CreateTags` is required because `RunInstances` and `CreateVolume` tag on
  create. Tag-on-create requires `CreateTags` with the `ec2:CreateAction`
  condition; the simplest correct grant is the unconditional `CreateTags` above.
  Tighten later with `"Condition": {"StringEquals": {"ec2:CreateAction":
  ["RunInstances","CreateVolume"]}}` if you want.
- `iam:PassRole` is NOT needed because the session instances get NO instance
  profile by default (see section 4). Add it only if you enable one.
- To constrain launches to the approved AMI/subnet/SG, add conditions on the
  `RunInstances` statement (e.g. `ec2:Subnet`, `ec2:SecurityGroup`,
  `ec2:InstanceType`). Keep them loose at first so a template parameter change
  does not silently break launches, then tighten once the values are stable.

## 3. VPC, subnet, and security group

The session instances only ever DIAL OUT. Nothing connects to them: the coder
agent dials the Coder server, and ao-worker dials the AO API. So no ingress is
required at all.

Subnet (`subnet_id` parameter):

- Any subnet in `eu-north-1` with outbound internet reachability.
- Public subnet with a route to an internet gateway (the template sets
  `associate_public_ip_address = true`), OR a private subnet whose route table
  points 0.0.0.0/0 at a NAT gateway (then set
  `associate_public_ip_address = false` in `main.tf`).
- Simplest is to reuse the subnet the Coder server already lives in.
- The durable EBS volume is created in the subnet's Availability Zone (the
  template reads it via `data.aws_subnet`), so a session's instance and its
  volume always land in the same AZ.

Security group (`security_group_id` parameter):

- Ingress: none.
- Egress: TCP 443 (HTTPS/WSS) and TCP 53 + UDP 53 (DNS) to `0.0.0.0/0` at
  minimum. TCP 80 is convenient for OS package mirrors but not required at
  runtime (the runtime is baked). The outbound destinations that matter are:
  - AO API and worker WebSocket: `https://staging-api.aoagents.dev`
    (`AO_CLOUD_PUBLIC_URL`).
  - Coder server (agent registration + PTY): `coder.aoagents.dev`
    (cloudflared tunnel, HTTPS/WSS).
  - `git`/`gh` against `github.com` for repository checkout.
  - Harness provider APIs (Anthropic, OpenAI, Cursor) that the coding agents
    call while running a task.
- A single "allow all egress" rule (`0.0.0.0/0`, all protocols) is the least
  fragile starting point. Narrow to the endpoints above once traffic is
  confirmed.

Example minimal SG (egress-all):

```
Ingress: (none)
Egress:  all protocols  ->  0.0.0.0/0
```

## 4. Instance profile for the session instances

None by default, and that is intentional: neither ao-worker nor the coder agent
needs AWS API access, so the session instances carry no credentials to steal.

Enable an instance profile ONLY if you add:

- SSM Session Manager (break-glass shell without SSH ingress): attach
  `AmazonSSMManagedInstanceCore`, and uncomment `iam_instance_profile` in
  `main.tf`, and grant the Coder principal `iam:PassRole` for that role, scoped
  to the exact profile ARN.
- CloudWatch agent for logs/metrics: add the relevant CloudWatch policy the same
  way.

If you enable a profile, add this to the section 2 policy:

```json
{
  "Sid": "PassSessionInstanceProfile",
  "Effect": "Allow",
  "Action": "iam:PassRole",
  "Resource": "arn:aws:iam::479575345906:role/ao-coder-session",
  "Condition": { "StringEquals": { "iam:PassedToService": "ec2.amazonaws.com" } }
}
```

## 5. Packer build permissions (build time only)

Building the AMI (RUNBOOK step 1) needs a principal that can run a builder
instance and register an image. Use the standard Packer minimum:
`ec2:RunInstances`, `ec2:TerminateInstances`, `ec2:StopInstances`,
`ec2:CreateImage`, `ec2:RegisterImage`, `ec2:CreateTags`, `ec2:DeleteSnapshot`,
`ec2:CreateSnapshot`, `ec2:Create*`/`Delete*` for the temporary keypair,
security group, and volume, plus the `ec2:Describe*` set. This is a build-host
concern, separate from the runtime policy in section 2. Run it under the
`ao-cloud` profile.
