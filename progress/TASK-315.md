# TASK-315: «Выполнено» — Wails binding and TS parsing

## Done
- `input/wails/homeworkdone.go`: `HomeworkService.MarkDone(ids, done)` and `RestoreDone(priorDone, prior)` return `DoneResult{problem "" | invalid | unavailable, outcomes[{id, code, done, version}], prior[{id, version}], priorDone}`; never an error. Per-ID codes are the use case's: changed | unchanged | not_found | deleted | conflict | failed (an unknown code or a missing id is `failed`). 1..50 ids else `invalid` without a call; a malformed id is `not_found` without a call; duplicates once. «Отменить» = `RestoreDone(priorDone, prior)` with exactly what the call returned (the page sends back ids, versions and one bool). A negative version (the use case's «no check» sentinel) or malformed id in `prior` is `not_found` and never reaches the use case. No journal, no Google.
- `ListAssignments(ctx, state)`: `"" | open | done | all` through `ListAssignmentsByState`; unknown state is `ErrHomeworkListState`. `AssignmentItem` has `done`, `doneAt` (also in `OpenAssignment`). `TodayPost.homeworkDone` (count, from `PostView.HomeworkDone`).
- `bindings/homework.ts`: types, `listAssignments(state = "open")`, `markDone`, `restoreDone` (never reject, `parseDoneResult` never throws), `doneMessages` (Russian texts per code, conflict explained), `doneSummary(res)` (counts, distinct messages, `undo` only when a prior exists). `bindings/today.ts`: `homeworkDone`.

## Decisions to confirm
- `TodayPost.homeworkDone` is a count (the view's `HomeworkDone` is an int, "of them done"), not a bool; TASK-316 can use `homeworkDone === homework`.
- A missing `done` / `doneAt` / `homeworkDone` is read as false / "" / 0 (older shape; keeps the existing fixtures); a wrong type still throws.
- Result codes follow the supervisor's TASK-314 decision (`conflict`, not `busy`); the tasks.json text of TASK-315 and TASK-316 («busy», «Повторить») is stale. A retry of a conflicted undo conflicts again: TASK-316 should offer «open the assignment», not «Повторить», for `conflict`.

## Follow-ups
- TASK-316: render the toast from `doneSummary`; refresh the list on `not_found`/`deleted`.
- TS `parseDoneResult` does not check duplicate prior ids nor that every `changed` outcome has a prior entry (Go guarantees both) — optional hardening.
- `doneMessages.unavailable` is the generic text; TASK-316 may word it per action.
- Readers still open-only: `input/agent/reply.go`, `input/wails/agentmentions.go` (TASK-320).

## Changed existing tests
- `input/wails/homework_test.go`: the fake has `ListAssignmentsByState`, MarkDone/RestoreDone; the all-methods table gets both; JSON expectations gain `done`/`doneAt`; `ErrHomeworkListState` in the error list.
- `lesson_test.go`, `app/homework_test.go`: `ListAssignments(ctx, "")` (new signature).
- `settings_test.go`: HomeworkService method count 21 -> 23.
- `frontend/test/lesson.test.mjs`: the AssignmentItem fixture gains `done`, `doneAt`.

## Security
Security-auditor: no findings. Reviewer: approve; Medium (conflict text told an un-marking user to «mark again» — reworded; TASK-316 `busy` wording stale — recorded above), Lows: not_found text got «: обновите список.»; others in follow-ups.
