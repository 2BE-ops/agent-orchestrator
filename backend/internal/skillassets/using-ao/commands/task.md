# Persistent tasks and worker output

`ao task` and `ao knowledge` call the daemon and print JSON. Use `--help` for flags.
Task planning, worker claims, transport delivery and evaluated completion are
separate facts. Workers follow the frozen criteria supplied with their attempt.

Read task state with `ao task show <task-id>` and attempts with
`ao task attempts <task-id>`. Inspect the exact received context with
`ao task context <task-id> <attempt-id>`; the manifest includes source hashes and
reasons for omitted content. `ao knowledge list <project>` lists knowledge;
`ao knowledge show <id>` and `ao knowledge version <id> <version>` retain status
and provenance. Candidates and invalidated entries are not accepted project facts.

Reserved workers receive an AO output protocol in their launch prompt, with the
correct executable, session, task, attempt, generation and request examples.
Use its JSON argv as literal arguments and send one JSON request on stdin:

- `ao task submit-result <session-id> --file -` takes `sourceGeneration`, a stable
  `idempotencyKey`, `expectedVersion` (0 initially), and `definition`. The schema-v1
  definition records summary, implementation, decisions, assumptions, interfaces,
  tests, findings, unresolved issues, recommended follow-up and knowledge candidates.
  `claimedOutcome` is completed/partial/blocked; it does not complete the task.
- `ao task send-message <session-id> --file -` takes `sourceGeneration`, a stable
  `idempotencyKey`, and `definition`. The schema-v1 definition requires kind,
  another task's `targetTaskId` in this project, subject, body and `correlationId`.
  Kinds: finding, question, answer, blocker, handoff, interface_contract,
  review_request and dependency_update. Answers reference the question's
  `replyToId`; interface contracts include `interface: {name, contract, files}`.
- `ao task results <task-id> <attempt-id>` inspects retained result versions.
  Corrections use a new key and the latest result number as `expectedVersion`.
- `ao task messages <project> --task <task-id>` observes the shared timeline.
  `ao task message <project> <message-id>` separates content from delivery history.

Retry an identical request with the same key after a lost response. Never change
content under a used key. Results permit 16 versions of at most 256 KiB each;
messages permit 256 per attempt, at most 32 KiB each. The launch protocol supplies
field limits. All reported tests remain claims for independent evaluation.

Keep the supplied source generation. An ownership conflict means preserve output
and await the replacement generation's instructions; do not look up a new owner
and submit on its behalf. Historical prompts never refresh this authority.
Transport `handed_off` acknowledges a native handoff, not worker receipt. Do not
repeat an uncertain delivery as a new message or act twice on the same message ID.
Historical interface proposals in context are not fresh action requests.

Inspect independent evidence with `ao task evaluations <task-id> <attempt-id>`
and `ao task evaluation <task-id> <attempt-id> <evaluation-id>`. Each snapshot
retains its result, criteria, exact commit, source observations and configuration
attribution. Worker claims do not set its verdict. A user or authorized controller
can request collection with `ao task evaluate <task-id> <attempt-id> --file -`:
`{"resultId":"...","expectedVersion":0,"idempotencyKey":"...","reason":"..."}`.
Use a new key and the latest evaluation number for a fresh assessment; exact
retries return the original evidence. Stored CI evidence does not imply a new fetch.
