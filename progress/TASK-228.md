# TASK-228 — assignment soft delete and «Вернуть»

## Done
- Domain `coursework/assignment`: `Delete(now)`, `Restore(now)`, `deletedAt` in State, `ErrDeleted`, `ErrNotDeleted`; both bump the version. A deleted assignment is not edited (Edit, Correct, Reextract, SetHints, Superseded are no-ops or ErrDeleted); `SameByStudent` is false for it, so a repeated create does not revive it.
- Migration `0039_assignment_deleted.sql` (`assignments.deleted_at INTEGER NULL`), mirrored in docs/contracts/schema.sql; sqlc v1.31.1 regenerated.
- `homeworkflow.DeleteAssignment` / `RestoreAssignment(ctx, id, expectedVersion)`: one transaction, one aggregate; `ErrInUse` via the new port `AssignmentRepository.Busy` (delivery PENDING/SENDING/UNKNOWN, work in triage/writing, active homework_draft/work_writing job naming the assignment).
- Readers skip deleted: list queries filter in SQL; `Service.Assignment` hides deleted (ErrNotFound wrapping ErrDeleted); Assignments list, Today, 7-day list, subject page, summary view, agent `list_assignments`. Every agent tool naming a deleted assignment gets `unknown_id` (guard in `input/agent` + propose_deadline, propose_send, generate_draft in homeworkflow).
- Reprocessing a recording or material neither creates nor revives a deleted assignment (tombstone).
- Nothing decides ADR-0029 Open C-2 (no purge, retention or TTL); Open C-1 untouched.

## Decisions to confirm
- «разбор» read as work status triage; works in questions/review do not block delete.
- Files outside the task list: `input/agent/*` (write.go, actions.go, assignment.go, tools.go, propose.go) for `unknown_id` and Undo refused as `changed` after a delete (required by the ADR); `cases/homeworkflow/proposals.go` `facts()` uses `liveAssignment`: «Да» on an old proposal of a deleted assignment goes STALE/REJECTED `unknown_assignment` (fail closed). TASK-229 must switch it to `assignment_deleted`; start TASK-229 from master after this merges.
- Migration number is 0039 (0038 is subject_teacher, TASK-235).

## Follow-ups
- Not guarded yet: drafts, docx, turnkey writing, `LessonLabel` loading a deleted assignment by ID directly (UI/agent cannot reach a deleted ID now).
- Crash recovery: a create deleted by the student between crash and Recover settles as FAILED (practically unreachable).
- Guard in `input/agent` runs before journal replay: replay of an already-DONE call on a deleted assignment answers unknown_id (optional refinement).
- A VALIDATED proposal waiting for «Да» does not make `Busy` true (TASK-229).

## Changed existing tests
- `migrate_test.go` version 35 -> 39 (new migration); homeworkflow fakes (`fakes_test.go`, `postprocess_fakes_test.go`, `create_test.go`) gained `Busy` and skip deleted rows like the adapter; `input/agent/actions_fakes_test.go` can mark IDs deleted.

## Security
Auditor: no findings (no agent/LLM path reaches delete or restore; Busy and Save in one BEGIN IMMEDIATE transaction; static SQL). Reviewer: first round found the migration clash and agent tools accepting deleted IDs, both fixed and re-reviewed.
