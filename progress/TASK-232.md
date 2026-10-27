# TASK-232 — «Удалить» / «Вернуть» / «Изменить» on assignments (UI)

Branch ralph/20261027-task-232. Frontend only.

## Done
- «Удалить» without a dialog on every row of «Задания» and on the assignment page; in its place a line «Задание удалено» + «Вернуть» until the student leaves the page. A synchronous per-id `flightGuard` gives one binding call on a double click.
- «Изменить» on the assignment page: the add form's fields are now a shared `AssignmentFields`; prefilled (local date/time, 23:59 = no time), field errors by `field`, Enter/Esc as in the add form.
- Russian texts for in_use, changed (data reloaded), deleted, not_found, failed, subject_not_found, invalid per field; an untouched LLM note with bidi/control characters shows a field error at «Заметка».
- Subject prefilled by name. Lesson-linked assignment (`lessonId` or `subjectFromLesson`): the empty option reads «Предмет занятия». Unmatched subject name: field error and «Сохранить» blocked until the student picks an option (otherwise a rename would silently clear the correction).
- «Сегодня» and «Горит на 7 дней» drop the deleted assignment on the next open (no cross-page refresh).
- `styles.css`: one rule `.hw-removed` (outside the listed files).

## Decisions to confirm
1. Saving is disabled until library subjects load (and for good if the load fails), so an empty choice cannot clear the subject.
2. The edit form does not require a date; clearing it removes the deadline (core decides; deadline limits Open 3 untouched).
3. «Предмет занятия» is shown whenever lessonId is set, even if the lesson has no subject (TASK-231 decision 2).
4. Nothing decided about ADR-0029 Open C-2.

## Follow-ups
- Text for `assignment_deleted` on a stale «Да» in the agent panel (agentconfirm.go, out of scope); the send dialog already has it.
- Subject id in the DTO would remove the prefill by name (duplicate names take the first).
- Low: a reload racing a delete on the assignment page can show the deleted assignment as live until the next action (refused as changed); form `subjectId` is not re-matched after a stale reload; move «Выберите предмет» into `changeTexts`.
- Dark theme, focus after delete/restore, Enter/Esc: checked only by SSR/pure tests.

## Changed existing tests
None (`AddFieldErrors` gained a `subject` key). New: test/homeworkedit.test.mjs. 881 tests pass, build OK.

## Security
Auditor: no findings. Reviewer: first round 1 medium + lows fixed; re-review approve.
