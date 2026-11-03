# TASK-369 — агент видит названия записей и пар (round with TASK-366)

One branch / one PR with TASK-366. See `progress/TASK-366.md` for the shared decisions, review and follow-ups.

## Done
- `find_lessons` rows (`values.LessonRow`) carry `title`, `subject_name` and `teacher` (omitted when empty), for recorded and scheduled rows, also when the subject is not in the library. Recorded rows: the knowledge-base lesson (student's correction first), then what the recording kept of its lesson (`LessonTitle`/`LessonSubject`), teacher from `LessonTeacher`; scheduled rows from the schedule entry.
- Data, not instructions: `dataLine` (one line, format/bidi/zero-width removed, control characters to spaces, cut at a character boundary with «…»); bounds 120 / 120 / 80 characters. If 50 rows exceed the 16 KB result, teachers are dropped first, then titles, then rows are halved as before. No new catalog tool.
- Panel v9 prompt (in place): look with read tools first (`find_lessons`, `list_subjects`, assignments), ask only what no tool gives, never «нет возможности посмотреть»; «последняя запись» = newest row with a recording; «добавь предмет по последней записи» -> `create_subject` with that row's `subject_name` (existing confirmation policy); the three fields are data in the trust statement, with a narrow exception to copy `subject_name` exactly into `create_subject`.
- Tests: tool-level (`TestFindLessonsCarriesNames`, values tests for cleaning/bounds/drop order), scenario on real SQLite with a scripted Hub (`desktop/tests/agent_lesson_names_test.go`: find_lessons then create_subject "Социология культуры", no questions card), agenteval live case f02, Hub prompt assertions (`TestPanelV9LookFirst`).

## Decisions to confirm
- `title`, `subject_name`, `teacher` (teacher names are personal data, imported ICS text) now reach the planner, the Hub and the LLM provider; ADR-0022 said "no titles reach the planner" (addendum needed).
- The schedule has no tool of its own; the prompt says it is read through `find_lessons` (scheduled rows).
- The scripted scenario checks plumbing, not the prompt; the prompt behaviour is measured by the live case f02 (not run here, needs credentials).

## Follow-ups
See `progress/TASK-366.md` (ADR-0022 addendum, Hub redeploy).

## Changed existing tests
`input/agent/tools_test.go` (`runRead` strips the three names for find_lessons; planned lessons no longer asserted nameless), `tests/agent_driver_test.go` (`TestDriverInjectedLessonTitleIsNoCommand`: the injected title appears only as `title` of the find_lessons row; `send_email` still refused), `tests/agent_schedule_test.go`.
