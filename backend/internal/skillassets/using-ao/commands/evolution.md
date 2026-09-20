# ao evolution

Run controlled Agent Type/Skill version experiments and inspect persisted
recommendations. Everything is project-scoped and JSON output.

## Sealing an experiment

```bash
ao evolution experiment-create <project> --file experiment.json
```

`experiment.json` pins the comparison before any evidence is read:

```json
{
  "kind": "agent_type",
  "entryId": "rust-coder",
  "controlVersion": 3,
  "candidateVersion": 4,
  "hypothesis": "Rust-v4 reduces review revisions",
  "minimumSamples": 12
}
```

Both versions must already exist as immutable versions of the same entry. The
pin is sealed at creation: nothing about the comparison can be edited later.

## Reading experiments

```bash
ao evolution experiments <project> [--after <id>] [--limit 1-100]
ao evolution experiment <project> <experimentId>
ao evolution experiment-diff <project> <experimentId>
```

`experiment-diff` returns the complete field-level diff between the pinned
control and candidate versions. The versions are immutable, so the diff is
stable across restarts.

## Evidence and conclusions

```bash
ao evolution evidence <project> <experimentId> --from <RFC3339> --to <RFC3339>
ao evolution conclude <project> <experimentId> --file conclusion.json
```

`evidence` derives both comparable cohorts from durable attempt facts for the
admission window: attempts pinned to exactly the control or candidate version.
Mixed-configuration and unseeded attempts are excluded with separate counters,
and tasks that fed both cohorts are reported as confounds. Nothing is stored;
the same window read after a restart returns the same answer.

`conclusion.json` carries the decision, and the daemon recomputes the evidence
itself before sealing — evidence is never accepted from the caller:

```json
{
  "outcome": "promote_candidate",
  "reason": "Both cohorts cleared the minimum with no confounded tasks",
  "from": "2026-09-12T00:00:00Z",
  "to": "2026-09-19T00:00:00Z"
}
```

A `promote_candidate` outcome is refused unless both cohorts reach the
experiment's minimum comparable attempts and no task fed both cohorts; one
successful task is never sufficient evidence. The sealed promotion path
applies the entry's manager-versioning policy: a Manager promotion under a
permissive policy records `version_permitted` (the version is still created
through normal registry authoring); under a restrictive policy it records
`recommendation_required`. Experiments conclude exactly once.

## Recommendations

```bash
ao evolution recommend <project> --file recommendation.json
ao evolution recommendations <project> [--after <id>] [--limit 1-100]
ao evolution recommendation <project> <recommendationId>
ao evolution recommendation-diff <project> <recommendationId>
ao evolution decide <project> <recommendationId> --file decision.json
```

`recommendation.json` persists an improvement proposal with its observation,
sample size and a full proposed definition:

```json
{
  "kind": "skill",
  "entryId": "reviewer",
  "fromVersion": 2,
  "observation": "Six of eleven recent tasks required revision",
  "sampleSize": 11,
  "proposed": { "skill": { "instructions": "Require component tests", "capabilities": ["review"] } }
}
```

`recommendation-diff` shows the inspectable changes from the sealed
from-version to the proposal. `decision.json` seals the one-time disposition:

```json
{ "disposition": "adopted", "reason": "Create the version through registry authoring" }
```

Adoption is a recorded decision, not a mutation: versions are created only
through the normal registry authoring flow, and a decided recommendation can
never be rewritten or decided again.
