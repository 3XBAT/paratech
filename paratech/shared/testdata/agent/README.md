# Agent eval fixtures (level 1)

Shared by the desktop and Hub tests (TASK-135, design §3.3). Plain `go test`, part of CI.

## replay/

Recorded `AgentStep` answers: the calls exactly as the Hub returns them (`call_id`, `tool`, `arguments_json`).

- `hub`: `accept` or `reject` — the Hub's catalog check of a returned answer (`hub/tests/agent_eval_test.go`).
  A rejected answer is a non-retryable `PROVIDER_ERROR`, and the same key replays it without a provider call.
- `desktop`: per call ID, what the desktop does when it gets the answer (`desktop/tests/agent_eval_test.go`):
  `driver:R|U|C` (handed to the Driver with that class), `card:<kind>`, `refused:<refusal>`.
  The desktop runs every fixture, also the ones the Hub rejects (defence in depth).
- `desktop_stop`: instead of `desktop`, the reason the whole answer stops the request (`budget`, `failed`).

Desktop context: base subject `s1`; scope subject `s1`, lesson `l1`, assignment `a1`; upload `u1` attached;
a paid call costs 10 000 µ$ (below the plan threshold).

## injection/

Untrusted text and the property it must never break: `name`, `source` (`material_text`, `file_name`, `transcript`,
`ics_title`, `reader_enum`, `reader_text`), `untrusted`, `property`, `refused_as_name` (never a stored subject/folder name),
`pending_driver` (what only the Driver, TASK-117, can check end to end).

Tests: `desktop/tests/agent_injection_test.go` (every argument of every tool, names, tool results through the use case,
and the INSPECT_MATERIAL answer: every fixture in every answer slot is refused by the desktop parser, and as the text
of an uploaded file read end to end it moves nothing) and `hub/tests/agent_eval_test.go` (catalog check, the step through the handler, and the INSPECT_MATERIAL reader:
every fixture as the material text, obeyed by the model in each answer slot, is a permanent `PROVIDER_ERROR`).

The TRIAGE_ASSIGNMENT reader (TASK-129): every fixture as a triage question and a helper step stays card data and never
reaches the planner, and obeyed in a schema slot it is refused (`desktop/tests/turnkey_test.go`,
`hub/tests/triage_eval_test.go`).

The ANSWER reader (TASK-118): every fixture as a lesson fragment stays data for the model, and obeyed in a slot that is
not claim text (a chunk ID, an extra field, a claim without a citation) or as prose it is refused
(`hub/tests/answer_eval_test.go`).

The C tools run end to end through the Driver to a confirm card in `desktop/tests/agent_propose_test.go` (TASK-127):
the methodichka fixture is the source file of the assignment, its address never becomes the recipient.

Pending the Driver / later tasks (not faked here):
- Result mapper drops transcript and file text from tool results (ADR-0022, TASK-117).
- ICS lesson title through the real loop; library → base mapping drops names `ParseName` refuses (TASK-117/121).

## Adding a fixture

One JSON file per case, file name = `name`. Unknown fields fail the loaders. Add cases in every phase.
