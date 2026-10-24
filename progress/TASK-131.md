# TASK-131: write_work (outline -> sections -> GOST .docx), absorbs TASK-105 core

Status: done (PR open). TASK-105 stays `blocked` as is (its description already says it is absorbed by 131/135).

## Done
- Domain: `work` aggregate got a writing state (outline, sections, sources with `verify`, generation, approval, attempts/revisions, file). Strict `ParseOutline`/`ParseSection`; stored state re-validated on load.
- Outline first, then the pause: after the outline is stored the work waits (`StepApprove`); only the student's `ApproveOutline` (Wails `TurnkeyService`: StartWriting, ApproveOutline, EditOutline, Regenerate) starts the sections job. The agent's `Turnkey` port has no student moves (reflection test).
- One section per queue step (`ErrContinueLater`), progress «Раздел N из M»; stored sections are never asked again (StepKey = work, step, section, revision, attempt, hash of data).
- `Work.Instruction()` replaced by `Work.Brief`: reader-written assumptions/questions go to the writer as one JSON data field, not as instruction (supervisor fix). Student note is a separate block.
- .docx via the TASK-104 builder (`homeworkdoc.BuildPaper`, GOST, title page from profile + lesson label, bibliography, «Сгенерировано — прочитайте перед сдачей»); literature only from the student's materials, else «проверить источник».
- Filed into the subject's «ДЗ» (`library.FileHomework`); Undo moves it back to «Входящие» (file kept). Undo of an older generation is a no-op (target changed). Regeneration moves the previous paper back to «Входящие».
- Delete assignment cancels the `work_writing` job (recordingdelete) - tested. Blocked outline recoverable by `Regenerate(0, note)`.
- Migration 0028 (works.writing), after master's 0027_answers. ADR-0025 addendum.
- Catalog: untouched (write_work was already in catalog v1/v2 from TASK-117/118 sets); nothing to port.

## Decisions to confirm
- write_work job ends at the outline; the sections job is started by the student's approval (job key `...:a`). Undo after the outline is "target changed".
- Student EditOutline of a done work counts as approval.
- Writer model alias is `default` on `homework_draft` (app/jobs.go); reviewer asked to confirm the Hub alias and max output for long sections.
- No Hub change: reuses the existing draft operation.

## Follow-ups
- Frontend (TASK-132): outline editor/approve UI, «план готов, утвердите» for a completed outline job, do not show Writing.Material as current while a step runs, ResultCard emission (no production ResultCard yet).
- sqlc was not available on the dev machine: drift check for works.sql / sqlcgen left to CI.
- Lows: `unfile` is check-then-act outside a transaction; orphan paper possible after a crash during a concurrent outline edit; subject for «ДЗ» comes from LessonLabel (transcript-derived hint can mis-file locally); no-op EditOutline still bumps stored version.

## Changed existing tests
sqlite/migrate_test (version 28), sqlite/agent_test, sqlite/works_test, app/registrations_test (work_writing job type), cases/turnkey fakes/service/writing tests, domain work_test/writing_test, action_test, input/agent triage_test, input/wails turnkey_test (writing field), tests/turnkey_test, tests/recordingdelete_sqlite_test (optional handlers).

## Security
security-auditor: no High/Medium; one Low (mis-filing via LessonLabel). Reviewer: 1 High (no outline pause), mediums fixed and re-reviewed; last re-review finding (blocked outline) fixed.
