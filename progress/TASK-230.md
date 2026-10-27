# TASK-230 — edit an assignment after creation

Branch ralph/20261027-task-230. No migration.

## Done
- Domain `assignment/edit.go`: `StudentEdit` and `EditByStudent(e, now)`; rules depend on origin (student: ADR-0029 rules; extracted: `NewDetails` + bidi/control ban, tab allowed in note); sets `user_edited`; deleted -> `ErrDeleted`.
- `homeworkflow.EditAssignment` now takes `{ID, ExpectedVersion, Title, Note, Subject, Date, Time}`; zone from the use case clock; no time -> 23:59; subject name via the TASK-215 port, stored as the student's subject correction; unknown subject -> `ErrSubjectNotFound`; stale version -> `shared.ErrVersionConflict`.
- New `SetAssignmentDue` (exact instant, old rules) used by the agent `edit_due`, so its behaviour (title/description kept, Undo exact, no window) is unchanged; covered by an SQLite scenario test.
- Review fix: for student assignments the deadline window is checked only when the deadline changed (the form re-sends the current value). The limits themselves (now-31d .. now+2y, date-only 23:59) are unchanged.

## Decisions to confirm
1. Deadline limits (old ADR Open 3) are NOT answered by the owner; kept as they are.
2. Changing a deadline does NOT update an already-sent Google event: no existing mechanism does (deadlines reach Google only via a confirmed `create_deadline_event` proposal; an unsent confirmed proposal goes STALE). An "update event" flow needs its own task/ADR. Google stays only a view.
3. Empty Subject removes the student's subject correction; the teacher correction stays, so an extracted assignment tied to a lesson shows the lesson subject again. A real "no subject" needs a marker/schema change. TASK-232 should label «Без предмета» accordingly. Owner decision needed.
4. Extracted notes keep tabs allowed (one line in `checkExtractedNote` to ban).

## Follow-ups
- Event update flow for changed deadlines (see 2).
- `bidiControl` misses U+200E/U+200F/U+061C (low, pre-existing).
- `cases/homeworkflow/doc.go` still says EditAssignment is for extracted assignments (outside the task's file list).
- An untouched LLM note with bidi characters makes any edit fail with ErrInvalidNote: TASK-232 must show the field error.

## Changed existing tests
`homeworkflow/assignments_test.go` (new struct, field errors), `create_test.go`, `delete_test.go`, `lessons_test.go` (ExpectedVersion), `input/agent/actions_fakes_test.go` (SetAssignmentDue), `tests/agent_actions_test.go` (stub rename), `tests/agent_assignment_test.go` (ExpectedVersion/Date).

## Security
Reviewer: approve (2 mediums: window on unchanged due fixed; no-subject semantics recorded). Auditor: no findings; the agent write surface is narrower than before.
