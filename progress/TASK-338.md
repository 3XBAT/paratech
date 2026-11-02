# TASK-338 — deleting a «busy» assignment

## Done
- Delete is refused (`ErrInUse`, code `in_use`) only while a delivery is PENDING, SENDING or UNKNOWN. `AssignmentRepository.Busy` is now deliveries only.
- New ports in `cases/homeworkflow`: `AssignmentActivity` (`Working`, `ActiveJobs`; sqlite, two new sqlc queries) and `JobCanceller` (app wires `runnerCanceller`). `Service.DeleteAndStop` (`DeleteAssignment` wraps it) commits the soft delete, then cancels every active job naming the assignment, after the commit and outside the transaction; a cancel failure is logged, never undoes the delete. `Service.DeletionImpact`.
- Handlers fail closed for a deleted assignment: `Drafts.Handle` (start and before store, `ErrDraftAssignmentGone`) and `turnkey.Handle` (start, before each section save, before filing; `ErrWriteGone`). Closes the TASK-228 follow-up for drafts and turnkey writing.
- Wails (additive): `ChangeResult.stopped`, new `HomeworkService.DeleteImpact(id) {working}`.
- UI: both delete entry points (list row, detail page) ask `deleteImpact` first; a working assignment shows an inline warning «Работа над заданием будет остановлена…» with «Удалить»/«Отмена»; otherwise delete is one click as before. Removed line adds «Работа остановлена». Refusal text «Письмо преподавателю ещё отправляется — удалить можно после» + «Открыть отправку» (list: opens the assignment scrolled to «Письма»; detail: scrolls and focuses it).
- ADR-0029 Addendum 4.

## Decisions to confirm
- PENDING delivery (a scheduled one too) still refuses: a delivery has no cancel in the domain; adding one means a new status, migration and ADR. PENDING lasts moments, so the refusal is short-lived. A cancel of a scheduled send would be a follow-up.
- Refusal text is the one from the task for all three states; for UNKNOWN (needs the student to reconcile) and a long-waiting PENDING "удалить можно после" is imprecise; distinct texts need the reason in the binding (follow-up).
- Restore does not restart the work: the work stays in `writing` with no job; `StartWriting` continues it.

## Follow-ups
- A triage call in flight is not a job and may still save its answer to the work of a deleted assignment (no outside effect). `Stopped` is also true for a work in triage.
- Small windows between the liveness check and the store in the handlers: one section, draft version or library file may be stored for a just-deleted assignment (whole writes, local; come back with «Вернуть» or are purged). A cancel between `FileHomework` and the final save can leave a complete .docx/.pptx in «ДЗ» (same as `StopWriting` before).
- Finished `homework_draft`/`work_writing` job rows keep the student's instructions after purge (TASK-274 follow-up).
- Merge risk with TASK-315: `input/wails/homework.go` interface line `DeleteAssignment` replaced by `DeleteAndStop`/`DeletionImpact`; `settings_test.go` HomeworkService method count 21 -> 22.
- The page flows have only source-pattern tests (no DOM harness in the repo).

## Changed existing tests
- `adapters/sqlite/assignments_deleted_test.go` `TestAssignmentBusy`: works/jobs now expect Busy=false (and `Working`/`ActiveJobs` true).
- `input/wails`: `homeworkchange_test.go`, `homeworkchange_expired_test.go` (`"stopped"` in JSON), `homework_test.go` (fake methods), `settings_test.go` (method count).
- Frontend: `homeworkchange.test.mjs`, `homeworkedit.test.mjs` (in_use text, page-source pattern), `homeworkrestoreexpired.test.mjs` (`stopped:false`).

## Security
Auditor: no findings (Busy and Save in one transaction, cancel after commit only for the two job types, static SQL with exact payload match, no agent/LLM path to delete or cancel). Reviewer: approve; fixed the ADR text, «Открыть отправку» landing, busy label and focus; lows listed above.
