# TASK-180: send a finished work to the teacher (core)

## Done
- Proposal kind `send_work_email` (formats docx|pptx), built only by `proposal.NewWorkSend` from the student's action; the model path refuses it. Library file pinned by ref `materials/<sha>`, SHA-256 and size; Recheck goes STALE on changed/moved/deleted file, recipient, subject or format.
- `homeworkflow.ProposeSendWork` calls `Work.Sendable()` and maps `ErrNoAssignment` to `no_assignment` (nothing stored). Recipient only from settings. On «Да»: re-check, copy to `exports/sent/<proposal id>/<safe name>.<ext>`, send through `senddelivery.SendHomework` (key `proposal:<id>`, UNKNOWN never resent). `cases/senddelivery` untouched.
- `AgentConfirmService.ProposeSendWork(workId)`, binding `proposeSendWork` + `noAssignmentText`, ADR-0025 addendum.
- New domain service `domain/library/services/filing` (shared by `library.fileInto` and homeworkflow) after review found that a file moved to another subject's «ДЗ» counted as filed.

## Scope expansion (approved by the supervisor)
Migration `0033_work_send.sql` (rebuilds `proposals`: kind CHECK, `pptx`, `work_id`), sqlite proposals adapter, queries, sqlc, `docs/contracts/schema.sql`, `MediaTypePptx` in `domain/delivery`, `desktop/tests/schema_test.go`. Renumbered 0032 -> 0033 because TASK-188 took 0032 (merged origin/master).

## Decisions to confirm
- "Filed in ДЗ": file is in the «ДЗ» of the work's own subject; for a work without a subject (or whose subject is gone) it must be in «Входящие».
- `no_assignment` is a refusal, not a stored reason. The card reuses kind "send" for the UI.
- `proposal.Kinds()` = kinds the model may propose; `AllKinds()` adds `send_work_email`.

## Follow-ups
- UI button (calls `proposeSendWork`); «Сегодня» shows the raw kind `send_work_email` for a waiting work send.
- Copies in `exports/sent/` are never cleaned up.
- The attachment name follows the library name at execution (a rename between card and «Да» changes the file name, content is pinned); bidi characters are not stripped by `SafeFileName`.
- After an executed send, a new send of the same work is allowed with a fresh «Да»; no "already sent" warning (UI task).

## Changed existing tests
`migrate_test.go` (version 33), `delivery_test.go` (pptx cases), `desktop/tests/schema_test.go` (kinds vs `AllKinds`), one helper swap in `TestSendWorkWithoutSubjectFromInbox`.

## Security
Auditor: no findings (no LLM path, Confirm cannot be bypassed, pinned content sent, safe copy name, no resend after UNKNOWN). Reviewer: one major (cross-subject move) and one medium (ARCH-22) fixed, re-review approved.
