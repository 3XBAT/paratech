# TASK-429 — recorded lessons in the subject's folders, core and binding

## Done
- `cases/knowledgebase`: `FolderLesson{Folder, System, Lesson}` (types.go) and `Service.FolderLessons` (folders.go). Flow: validate id (`ErrInvalidRequest`), read the subject list (unknown/removed → `[]`), `Lessons` read (order newest first, no notes), keep `matched` to the subject, folder = `filing.Target(sub, s, filing.NoteFolder(kind))` (corrected kind wins via `kb.NewLesson`), not ok → left out. No new port, no migration, no new rule in the use case.
- `input/wails`: `KnowledgeFolderLesson` DTO and `KnowledgeBaseService.FolderLessons`; invalid id → `ErrKnowledgeInvalidSubject` «Неверный идентификатор предмета.»; any other failure → `ErrKnowledgeUnavailable` (cause logged only); empty list is `[]`.
- `frontend/src/bindings/knowledgeBase.ts`: `methods.folderLessons`, `KnowledgeFolderLesson`, `parseFolderLesson` (rejects bad `system`, empty `folderId`, anchor not `recording`), `errorMessages.invalidSubjectId`.
- `filing/note.go`: doc comment only.
- Tests: fake table + order + failures (`folders_test.go`), exact JSON and errors in `knowledgebase_test.go`, `test/folderLessons429.test.mjs` (node:test, the repo has no vitest), real-SQLite scenario `desktop/tests/folderlessons_sqlite_test.go` (lecture → «Лекции», SetLessonInfo seminar → «Семинары», marked for deletion → no row, schema version unchanged).

## Decisions to confirm
- «Subject without that system folder → absent» has no test: the subject aggregate requires all six system folders (`subject.New`/`Restore`), so the case cannot be built; the `!ok` branch is defensive.
- Schema version is compared before/after, not pinned to a number (a pinned number would break every later migration task).
- Frontend test is `node:test`, not vitest (as the other binding tests).

## Follow-ups
- TASK-430 (UI) consumes `folderLessons`.
- Reviewer lows: `l.Library.ID != sub` in folders.go repeats the `LessonsOf` key filter (harmless, documents the rule); a subject removed between the list and the lessons read may show one stale row (read-only).
- The agent already lists lessons through `find_lessons`; no new agent tool needed (view only).

## Changed existing tests
`input/wails/settings_test.go` `TestFrontendCallsBoundNames`: exported method count of `KnowledgeBaseService` 8 → 9 (pins the count; `FolderLessons` is new). `knowledgebase_test.go`: fake gets the new method; `TestFrontendKnowsKnowledgeErrors` also checks the new error text. No assertion weakened.

## Security
security-auditor (opus): no findings (id validated before use, no cross-subject leak, fixed error texts, cause only in the log, read-only, no side effects). Reviewer (opus): approve, four lows (above, plus comment line length, fixed).
