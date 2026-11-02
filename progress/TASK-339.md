# TASK-339 — «Входящие»: actions on files, placement hint, drag, add file into a subject

## Done
- Core (Go): pure `domain/library/services/placement` (`For(fileName, candidates, proposals) Hint`): whole-word match of the file name against subject names and aliases (ё = е, longest wins, ambiguity = no hint, every occurrence checked), one marker table (ЛР/лаб, сем/пз/практ, лек, билет, дз) -> system folder, else «Материалы». `subject.SystemFolderName`. Use case `PlacementHint` (read-only; builds proposals from `schedulesubjects.Propose`, so «Создать предмет „Сети“» agrees with the schedule-subjects card: name without the lesson mark, spellings as `CreateAliases`; a file name never becomes a subject name). `Upload.Subject/Folder`: import straight into a subject (checks before storing; an already filed file is not moved; a failed placing after store returns `Imported.NotPlaced`). Wails: `PlacementHint`, `ImportFileTo` (new files `library_inbox.go`; `LibraryImport.notPlaced`). `docs/architecture.md` §12: `placement` in the library row.
- UI: one shared file row for «Входящие» and for folders: visible «Разложить» / «Открыть» + «⋯» (Открыть, Разложить…/Переместить…, Вернуть во «Входящие», Удалить), right click opens the same menu; inline placement panel (Предмет / Папка, «Новый предмет…»); hint line; soft delete with «Вернуть» lines (10 s, up to 3); drag the file icon to a subject card (custom MIME with only the ID, validated against the inbox); «Добавить файл» in a subject header and «+» per folder (results next to the source, «Переместить сюда», «Открыть предмет», «Повторить», «Обновить список»); «Входящие» above the subject cards; folder rows in the tree are buttons that scroll to their section.
- «Осень 2026» among folders: a display bug, not data (the core never returns a semester in `folders`): the semester row of the tree looked like a folder row; it is now a small-caps heading, subjects and folders are indented below it.
- Logic out of React into `materialFlow.ts` (MaterialFlow, HintScheduler, CardDrop, addFileInto) with behaviour tests.

## Decisions to confirm
- «Вернуть» after a «create» hint moves the file back to «Входящие» and removes the subject it created if the core still sees it empty (no files/notes); own folders, teachers or aliases the student added within the 10 s window go with it (reviewer M2). Alternative: keep the subject.
- The hint reads the schedule through `schedulesubjects.Propose` per call (capped at 200 rows, up to 20 serial calls per inbox render; ~0.2 s worst case each). Follow-up: compute once per batch or cache by schedule token.
- Folder from the file name only by markers; the extension never chooses the folder; no marker -> «Материалы».
- Drops of OS files on the window still go to «Входящие» (not into the open subject).
- Deleted files have no trash view after the 10 s undo window (follow-up «Недавно удалённые»).
- `placement` has its own marker table (the `schedulesubjects` marks are unexported and built for schedule strings).

## Follow-ups
- Go: reword «Неверный идентификатор: обновите список.» (ErrLibraryInvalidID) in plain Russian together with the TS copy in `bindings/library.ts` (`errorTexts`/`refreshTexts`).
- «Занятие не найдено в расписании.» maps to «Обновить список»; check that this is a real way out.
- Right click opens the menu at «⋯», not at the cursor, and suppresses the native menu on the name; focus after a place/delete (row unmounted) falls to body; drag handle is the file icon only.
- No DOM harness in the frontend: panel interaction, focus and scroll are verified through pure functions and SSR markup only.
- `ux-expert` (items 12-14 minor) and reviewer lows: not-placed note wording, `HintScheduler` re-attach test.

## Changed existing tests
- `test/library.test.mjs`: the tree folder count looks for `button.ti.fold` (tree folders are buttons now).
- `test/libraryInbox.test.mjs`: menu label «Разложить…», hint/undo/glossary texts, `panelChoice` no longer falls back to the first subject, drop cue «Сети → Материалы», regexp `errorWay` test replaced by a table test; several source-regex tests replaced by behaviour tests (`test/materialFlow.test.mjs`).
- Go: `placement_test.go` (marked schedule string now creates «Сети» with alias «Сети (лаб.)»), `inbox_test.go`, `library_inbox_test.go` (alias slice, DeepEqual).

## Security
Auditor: no findings in two rounds (file names only compared, never copied into names; drag payload validated; RemoveSubject only for a subject the flow created and only when the core finds it empty). Low from round one fixed (OS file drag always cancelled). Reviewer: round 1 one high (create-name kept the lesson mark) and four mediums, all fixed; round 2 and 3 approve. `ux-expert`: «доработать» with six must-fix items (inbox position, panel guessing a subject, honest undo of a created subject, glossary, result next to the folder, not-placed line), all fixed; it was run after the first implementation round (the agent type became available mid-task).
