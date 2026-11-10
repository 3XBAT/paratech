# TASK-430: lesson link rows in «Лекции» / «Семинары» / «Лабораторные»; counts include them

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-429 (binding `folderLessons`), TASK-428 (both edit `styles.css`)
- **PRD sections / AC:** ADR-0034 amendment 2026-11-10 (rule 3–4), ADR-0034 §2 (every row opens its object)
- **Агент:** view only (`docs/agent-parity.md` row added by the architect)
- **Branch:** ralph/<date>-task-430

## Goal
A held and processed pair shows in the folder of its kind on the subject page and in the tree counts, as a link to
its recording, so «Лекции —» / «Пусто: Лекции» no longer hide it.

## Design
- Read: `knowledgeBaseApi.folderLessons(subjectId)` next to `materials(subjectId)` in `SubjectsPage.tsx`, on the same
  triggers (subject opened, reload, changes) plus the existing re-reads of «Пары» (lesson screen left or corrected,
  recording imported). A late reply of another subject never shows.
- View logic in `libraryView.ts` (pure, tested): `lessonsOfFolder`, `folderCounts` and `foldEmptyFolders` count files +
  link rows; unknown link rows (not read yet / failed) → the three lesson folders make no «пусто» claim; section head
  «Лекции · 2 пары · 1 файл» (only the non-zero parts; plural «пара / пары / пар»).
- Row (`src/pages/FolderLessonRows.tsx`, new): recording icon, title or «Запись 21.10», muted «21.10 · 1 ч 20 мин», the
  quiet status of the «Пары» row while not processed (`subjectTabsView.ts` helpers, reuse). The whole row is a button
  that opens the lesson screen (default tab). Icon actions with tooltips (and the same `aria-label`):
  «Открыть конспект» → tab `summary`, shown only when `summaryReady`; «Открыть запись» → tab `transcript` (the audio
  player). No «⋯», not draggable, no checkbox. Link rows come before the files of the folder, newest first.
- Failure: a quiet line «Не удалось загрузить пары предмета.» with an icon «Повторить» (tooltip); files stay shown.

## Allowed to change
- `paratech/desktop/frontend/src/libraryView.ts`, `src/pages/SubjectsPage.tsx`, `src/pages/SubjectsParts.tsx`,
  `src/pages/FolderLessonRows.tsx` (new), `src/subjectTabsView.ts` (only to export a status helper), `src/styles.css`
  (row layout, if the file-row classes are not enough), `test/*`

## Out of scope
- `bindings/knowledgeBase.ts` (TASK-429), the lesson screen, «Записи», the agent files (TASK-418/426).
- TASK-427 (deferred by the owner) edits the same two pages; whichever comes later rebases.

## Acceptance criteria
- [ ] Subject with one processed lecture: tree «Лекции 1»; tab «Файлы» shows the section «Лекции · 1 пара» with the
      row; «Пусто:» no longer lists «Лекции». Seminar → «Семинары», lab → «Лабораторные».
- [ ] Row click → lesson screen; «Открыть конспект» → tab «Конспект»; «Открыть запись» → tab «Расшифровка»; both
      icons have tooltips; keyboard: Tab reaches the row and both icons, Enter/Space act, focus visible.
- [ ] After the recording is deleted (lesson screen or «Записи») and the student comes back, the row and its count are
      gone; after «Исправить предмет или тип пары» the row is in the new folder.
- [ ] Loading: no «пусто» claim for the three folders; failure: the line with «Повторить», files still shown.
- [ ] Owner rules: small actions are icons with tooltips; statuses are icon + colored text, no tinted fills or colored
      outlines; one accent; impersonal Russian texts as text nodes.
- [ ] UX check (coordinator-round.md 3a): discoverable, no dead ends, all states (loading, empty, rows, failure,
      processing), quiet UI.
- [ ] Tests: view logic (counts, fold, head text and plurals, unknown rows), row actions call the open handler with
      the right key and tab, failure line and retry, stale reply of another subject ignored.
- [ ] `npm --prefix paratech/desktop/frontend run test` and `run build` pass.

## Notes
- If the tab «Расшифровка» does not show the player before a transcript exists, report it in progress (no new player
  here; owner question В5 of the amendment).
