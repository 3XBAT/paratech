# TASK-350 — «хлебные крошки» над заголовком

## Done
Frontend only (no Go, no new dependency), pure view state.
- New `src/ui/Breadcrumbs.tsx`: `<nav aria-label="Путь">` with an `<ol>`; every level but the last is a `<button>` (tab order, global focus ring), the last is plain text with `aria-current="page"`; separator `/` is `aria-hidden`; labels over 24 chars get a `title`; all text is React text.
- Crumbs replace the back buttons and over-lines on: assignment page (`Задания / <название>`, also while loading / load error / deleted; was «Все задания» x3), Tickets (`Предметы / Билеты`), KnowledgeBasePage (`База знаний / <предмет> / <пара>`; the lesson crumbs call the old `closeLesson`/`leaveSubject`), KnowledgeLessonPage and NoteLecturePage (`trail` prop), Summary (`<откуда> / Конспект`) and Transcript (`<откуда> / Расшифровка`). Every crumb calls the callback of the removed button, so route history (Alt+←) is unchanged.
- SubjectsPage had no back button after TASK-341: only the «Библиотека» over-line was removed; semester and aliases are now one muted line under the h1 (`subjectSubLine`: «Осень 2026 · Также: матан, МА»).
- Tickets no-subjects empty state: no over-line, title «Сначала нужен предмет», button «Перейти к предметам». `EmptyState.label` became optional.
- Summary/Transcript get `parent` (label of the page underneath) from `App.tsx` (two JSX props + the `pageById` import; scope extended on the review's finding: a fixed «Записи» pointed to the wrong place from «Сегодня»).

## Decisions to confirm
- Crumbs are buttons, not `<a href>` (the app navigates by callbacks).
- Knowledge-base crumbs start with «База знаний» (the screen's current name); they move when TASK-354 folds it into «Предметы».
- Summary/Transcript crumb = `pageById(current).label` (page label, not the rail label).
- `src/App.tsx`, `ticketsView.ts`, `libraryView.ts`, `ui/controls.tsx`, `tsconfig.test.json` were touched outside the listed files (small, reasons above).

## Follow-ups
- Old back controls and over-lines remain outside the task's file list: `ConnectionsPage` (SectionPage «Назад»), `AppleCalendarHelpPage`, `TodayPage` «К сегодняшнему дню», over-lines on Journal, Settings, Diagnostics, Schedule, Models, Recordings (TASK-356 covers settings).
- Tickets crumb has no subject level until TASK-353 («Билеты к экзамену» from the subject head).
- Lesson crumbs «… / Пара 21.10» are finished in TASK-351; overlays go in TASK-352.
- `DoneIssueLine` «Все задания» (deleted assignment) is kept as the way out of a refusal; could be renamed «К списку заданий».
- The page tests are source-text regexes (no DOM harness); only the component is rendered.
- Check on screen: crumb alignment with the h1, ~32 px click target, focus ring not clipped at the left edge.

## Changed existing tests
None edited in place before the review fix. In the review-fix commit three assertions of the new `breadcrumbs350.test.mjs` were adjusted (own file, same task): summary/transcript crumb now uses `props.parent`, tickets empty state strings, merged subject sub-line. 1689 tests pass.

## Review
Reviewer: approve (medium: «Записи» crumb names the wrong place -> fixed). ux-expert: «доработать» (same crumb, doubled heading in the Tickets empty state, button label) -> fixed, plus merged sub-line and larger crumb target. Re-review not run: the fix commit is small and was checked by the coordinator. Security-auditor: not run — view-only change, no binding, route or trust boundary touched.
