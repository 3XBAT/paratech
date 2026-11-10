# TASK-430 — recorded lessons as link rows in the subject's folders (UI)

## Done
- `libraryView.ts` (pure, tested): `lessonsOfFolder`, `folderCounts` / `foldEmptyFolders` count files plus link rows, `folderHeadCount` («Лекции · 2 пары · 1 файл», non-zero parts only, пара/пары/пар), `folderLessonTitle` / `folderLessonMeta`, `FolderLessonsFlow` (open / reread / retry / close with a token against late replies).
- `pages/FolderLessonRows.tsx` (new): row button (opens the lesson screen, default tab) with sibling icon actions «Открыть конспект» (tab summary, only when `summaryReady`) and «Открыть расшифровку и аудио» (tab transcript, icon `wave`); quiet status chip from `lessonMarks`/`lessonChip` while not processed; «новое» mark; notice line with the «Повторить» icon.
- `SubjectsPage` reads `folderLessons` next to `materials` (open, reload, changes, lessonsSeen, lessonsEdited, recording import); `SubjectsParts` draws link rows before files and makes no «пусто» claim for the three lesson folders while rows are unknown; the tree shows no number for them then.
- Failure: first read → «Не удалось загрузить пары предмета.» + icon «Повторить»; failed re-read keeps the rows and shows «Список пар не обновился: изменения могут пока не отображаться.»; retry keeps the icon mounted (soft-disabled) so keyboard focus stays.
- Tests: `test/folderLessons430.test.mjs` (counts, fold, head and plurals, unknown rows, row actions with key and tab, failure/retry, stale replies of another subject, closed flow).

## Decisions to confirm
- Scope extension: `src/pages/SubjectTabs.tsx` (optional `onImported` prop, 2 lines) so a recording imported on «Пары» re-reads the folder rows and the tree count at once (the import counter lived there). Approved by the coordinator.
- The row title uses `lessonHeading` («Запись 21.10 14:05», as on the lesson screen) instead of the draft's «Запись 21.10»; then the meta line shows only the length («1 ч 20 мин») so the date is not shown twice. With a real title the meta is «21.10 · 1 ч 20 мин».
- Icon «Открыть запись» is `wave` with tooltip/label «Открыть расшифровку и аудио» (the app's `play` means «Запустить»); aria-labels carry the lesson name («<действие>: <название>»), tooltips stay short.
- The row button has a hint title: «Открыть пару. Предмет, тип пары и удаление — на экране пары, в меню «⋯».» (the row has no «⋯» by ADR rule 3).
- A failed read does not keep stale rows on the FIRST read; on a re-read it keeps them with the stale line.
- Row status comes from the overview marks (`lessonMarks`): an older lesson outside the overview window shows no chip (as on «Пары»).
- В5 check: `TranscriptPage` renders `<audio controls>` whenever `audioUrl` is non-empty, also before a transcript exists; no player added.

## Follow-ups
- UX-expert items not done: bare heads of the three lesson folders while rows load (jump), hint «записанные пары появятся здесь сами» in the empty folder, status of old lessons outside the overview window (needs the status in `KnowledgeFolderLesson`), loading/error line placed at the top of the tab, not under the folder.
- Owner question: «Открыть конспект» repeats the row click when the default tab is already «Конспект» (ADR rule 3 requires the icon).
- Focus fallback box after a successful retry is covered only by source tests; checked by hand in the QA steps.
- Processing that finishes while the subject page is open updates `summaryReady` / the chip only on the next trigger (same as «Пары»).

## Changed existing tests
`test/subjectTabs353.test.mjs`: the import-handler regex also requires `props.onImported?.()` (stricter, nothing weakened).

## Security
security-auditor (opus): 0 findings (titles only as text nodes, no URLs built, stale replies guarded, no side effects). Reviewer (opus): approve; fix round (stale re-read, focus on retry, title/labels) re-reviewed: approve, lows only. ux-expert (opus): «доработать» → must-fix 1–4 and 5, 6, 8 fixed.
