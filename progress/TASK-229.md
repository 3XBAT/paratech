# TASK-229 — consequences of deleting an assignment

## Done
- DeleteAssignment queues a `deadline_event_removals` row in its own transaction (ON CONFLICT DO NOTHING) and requests a calendar sync after commit; RestoreAssignment purges a still-waiting row. Existing sync deletes the event (404/410 = success) and the row.
- «Да» on a proposal of a deleted assignment (all four kinds) -> STALE, new reason `assignment_deleted` (`proposal.ReasonAssignmentDeleted`, STALE only; `Facts.AssignmentDeleted`). No CHECK on proposals.reason, so no migration. New proposals naming a deleted assignment stay REJECTED `unknown_assignment` (as in TASK-228).
- Recording deletion: `ListRecordingAssignments` filters `deleted_at IS NULL`; a deleted assignment is not offered, stays, transcript link NULL; naming it is `ErrInvalidRequest`.
- ADR-0029 Open C-2 untouched (no purge/retention).

## Outside the listed files (confirm)
- New ports `DeadlineRemovals`, `CalendarSyncRequester` in cases/homeworkflow (ports.go, types.go, service.go); optional, nil = event just stays in Google.
- Wiring in app/proposals.go and app/core.go; `sqlite.DeadlineEventRemovals.QueueDeadlineRemoval`.
- Tests in app/ and desktop/tests/.

## Changed existing tests
None (rows/tests only added; two lines added in worksend tests).

## Follow-ups
- Race: a CONFIRMED create_deadline_event past Recheck can re-create the event after a delete+sync (no removal row left). Same class as ADR-0018; accepted or fix later.
- UI message for `assignment_deleted` (input/wails maps all ErrStale to generic) -> TASK-231/232.
- Drafts/docx/turnkey/LessonLabel still load a deleted assignment by ID (execution re-checks before side effects).
- Agent wiring of delete/restore needs its own audit (rule 3: proposal + confirmation).
- Every delete queues a Google call, even without an event (404 = success).

## Security
Auditor: no findings. Reviewer: approve, no blockers.
