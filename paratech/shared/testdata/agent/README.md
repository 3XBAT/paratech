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
`ics_title`, `reader_enum`), `untrusted`, `property`, `refused_as_name` (never a stored subject/folder name),
`pending_driver` (what only the Driver, TASK-117, can check end to end).

Tests: `desktop/tests/agent_injection_test.go` (every argument of every tool, names, tool results through the use case)
and `hub/tests/agent_eval_test.go` (catalog check, the step through the handler, and the INSPECT_MATERIAL reader:
every fixture as the material text, obeyed by the model in each answer slot, is a permanent `PROVIDER_ERROR`).

Pending the Driver / later tasks (not faked here):
- C tool not executed end to end through the loop; `input/agent` has no Confirm/Execute/Send/Delete (TASK-117, TASK-127).
- Result mapper drops transcript and file text from tool results (ADR-0022, TASK-117).
- ICS lesson title through the real loop; library → base mapping drops names `ParseName` refuses (TASK-117/121).
- INSPECT_MATERIAL reader output on the desktop side (TASK-122 desktop part); the Hub side is enforced.

## Adding a fixture

One JSON file per case, file name = `name`. Unknown fields fail the loaders. Add cases in every phase.
