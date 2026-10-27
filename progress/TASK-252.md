# TASK-252 — the core gives the library subject of a lecture

## Done
- subjectmatch: `Index.Lookup` returns an explicit outcome (NoKey / NoMatch / Matched / Ambiguous); `Resolve` behaves as before.
- cases/library `LookupSubjects`; the knowledgebase `SubjectResolver` port now uses it; `kb.Lesson.LibraryMatch()` derives the state.
- `KnowledgeLesson` DTO: `librarySubjectId` (only when matched) and `librarySubjectState` (matched / none / ambiguous / no-subject). TS parser rejects unknown states and ids that do not fit the state.
- Frontend: `subjectKey` and name/alias matching removed; `pickSubjectId(notes, id, state)`; Russian hints for ambiguous / none / no-subject; the page no longer reads the library list.
- Ambiguous lessons keep their free-string group (grouping and export unchanged).

## Decisions to confirm
- Note-only lectures report `matched` (their note names the subject) or `no-subject`.
- Scope is recording-backed lessons (the page opens by recording id); pairs without a recording wait for TASK-155.

## Follow-ups (review lows)
- Ambiguous hint is a dead end (suggest renaming one subject in «Предметы»); the DTO/TS comments still say "the student chooses". Rare: only rows from before the duplicate-key refusal.
- The `no-subject` state reuses the «не найден» text; a whitespace-only subject (NoKey) shows as `none`.
- `LibraryMatch` constants are called wire names in the domain; mapping could move to input/wails. `subjectState` in OwnNotesModel could be required.
- TASK-153 also touches cases/knowledgebase: whoever merges second re-runs `go test -race ./desktop/internal/cases/knowledgebase/...`.

## Changed existing tests
- cases/knowledgebase `fakeResolver` implements `LookupSubjects` (port changed).
- wails `TestKnowledgeJSON` expects the two new fields; frontend `knowledge.test.mjs` fixture gets them.
- frontend `ownnotes.test.mjs`: the two old pickSubjectId tests (frontend name/alias/ё matching) replaced by tests of the new selection, hints and a source guard.

Security: 0 found, 0 fixed, 0 accepted, 0 doubted
