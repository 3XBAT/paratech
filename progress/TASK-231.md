# TASK-231 — Wails bindings: delete, restore, edit an assignment

Branch ralph/20261027-task-231. No migration, `app/` unchanged.

## Done
- `input/wails/homeworkchange.go`: `DeleteAssignment(id, version)`, `RestoreAssignment(id, version)` -> `{problem: '' | in_use | changed | not_found | failed, assignment}`; `EditAssignment({id, version, title, note, subjectId, date, time})` -> `{problem: '' | invalid | subject_not_found | changed | deleted | not_found | failed, field: title | note | due | time | '', assignment}`.
- Mapping: ErrInUse -> in_use; ErrVersionConflict and ErrNotDeleted -> changed; ErrDeleted/ErrNotFound -> not_found (edit: ErrDeleted -> deleted, checked first); field errors -> invalid + field (an untouched LLM note with bidi -> invalid/note); bad id -> not_found without calling the use case. A repeated «Удалить» with the old version gets `changed`, nothing is deleted twice.
- DTO `AssignmentItem` gained `version` and `subjectFromLesson` (subject source is schedule/transcript).
- No agent-journal entry; no agent tool for delete/restore (rule 3). `HomeworkProposals` widened by three methods; app passes the same service.
- ConfirmSend: a stale send now reads the proposal; reason `assignment_deleted` gives its own Russian error `ErrSendAssignmentDeleted`, otherwise `ErrSendStale`.
- `frontend/src/bindings/homework.ts`: `deleteAssignment`, `restoreAssignment`, `editAssignment` + parsers; they never throw, unknown reply -> `failed`. No Russian texts for the new codes (TASK-232).

## Decisions to confirm
1. On `failed` the cause is logged (as every sibling method; `TestHomeworkHidesCauses` requires it); known codes log code only. Nothing reaches the UI.
2. `subjectFromLesson` is exact only without a student subject correction (otherwise the view cannot tell if a lesson subject is behind it); it duplicates `subjectSource`. TASK-232 may use `lessonId != ""` as a hint. Depends on the owner's answer to TASK-230 decision 3.
3. The DTO has no subject id: TASK-232 prefills the subject by name (breaks on rename) unless a follow-up adds it.

## Follow-ups
- `input/wails/agentconfirm.go` still maps every stale proposal to the generic text (no `assignment_deleted` text for «Да» in the agent panel).
- TASK-232: Russian texts for all new codes and the field error for an untouched LLM note with bidi.

## Changed existing tests
`input/wails/homework_test.go` (fake methods, call-coverage list, JSON names, error list, stale ConfirmSend now expects an extra Get), `settings_test.go` (HomeworkService method count 18 -> 21), frontend fixtures `homework`, `homeworkadd`, `homeworkcreate`, `lesson` tests (new DTO fields; parser is strict).

## Security
Reviewer: approve, only lows. Auditor: no findings.
