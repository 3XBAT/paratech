# TASK-351 — один экран пары: маршрут `lesson` и вкладки

## Done
Frontend only (no Go, no binding, no new dependency); pure view state.
- `Route` got `lesson` (= `KnowledgeLesson.key`) and `tab`; `routeToLesson / routeToLessonTab / closeLesson`; every other route builder clears both. The lesson is an overlay over the page the student came from (like the summary overlay), so the page under it stays as left.
- History: opening a lesson is an entry; switching tabs is not (`sameRoute` ignores `tab`, `pushRoute` replaces the tab of the current entry). Alt+← leaves the lesson, Alt+→ returns on the last tab.
- `LessonScreen` (KnowledgeLessonPage.tsx) resolves the lesson: App's in-session cache (filled on every open), else `recording:<id>` through `lesson(recordingId)` (`recordingIdOfKey`). Note-only lectures also go through it (no tabs).
- Opening tab (`openingTab`): route tab, else «Мой конспект» if it has parts, else «Конспект» if ready, else «Текст лекции», «Расшифровка», else «Конспект» (so a running/failed summary, «Повторить» and the no-speech card are visible). Decided once after the notes read and the lesson re-read; a chosen tab is never replaced.
- Tab «Конспект»: `SummaryTools` = one «Скопировать» + «⋯» («Сохранить как текст (.txt)» / «Сохранить в Markdown (.md)»), shown only when the summary is ready; the storage paragraph and the «Открыть конспект» button are gone. Retry / redo / no-speech card moved into the tabs (otherwise a failed summary was a dead end).
- Head: «Исправить предмет или тип пары» in the head «⋯» (hidden while the form is open; focus goes into the form and back to «⋯» on close; «Предмет и тип пары сохранены.» status line).
- Crumbs: «Предметы / <предмет> / <пара>»; opened from «База знаний» they are «База знаний / <пара>» («База знаний» = back to the list/subject/search as left) until TASK-354.
- `KnowledgeBasePage` opens lessons through `onOpenLesson` (the only entry; no inline lesson any more); two counters from App re-read the lists/search after a lesson was left or edited.
- Duplicate headings under «Конспект» and «Задания» tabs removed (`heading={false}`); `ReadyMarks` and «Конспект paratech» label removed from the lesson page.

## Decisions to confirm
- Overlay (not a page of its own): the rail stays on the section the lesson was opened from.
- Crumb root follows the section under the lesson («База знаний» vs «Предметы») because «Предметы» has no list of pairs yet.
- «Конспект» tab vs button «Открыть конспект» (TASK-344 follow-up): the button is gone on this screen; the old one stays in recording rows until TASK-352.
- Touched `KnowledgeBasePage.tsx` outside the task's file list (the only entry to the lesson page).

## Limitation
There is no by-key binding in the core: a recorded lesson (`recording:<id>`) opens cold, a note-only lecture (`lesson:/day:/note:` keys) only from the in-session cache; otherwise the screen says why and offers «Назад». A by-key Wails binding should come before TASK-352 makes note-only lessons reachable from other screens.

## Follow-ups
- «Текст лекции» card keeps its own h2 (LectureTextParts.tsx); «Текст лекции» and «Расшифровка» tabs still have their own copy/save controls (unify with `SummaryTools`).
- Esc does not close `LessonInfoForm` (KnowledgeParts.tsx).
- `notReady` / `noSummaryHint` of `SummaryTools` are now effectively dead; remove with TASK-352.
- Delayed «Загрузка…» (no flash under 1 s); «Без предмета» crumb; the subject crumb from «База знаний» once TASK-354 has a subject list.
- `SummaryTab` keyed by tab: the busy lock is per tab (core refuses duplicates).

## Changed existing tests
rail348, summary, transcript (Route shape has `lesson`, `tab`); summary.test (tools component instead of the old toolbar text); breadcrumbs350 (lesson pages use `crumbItems`, knowledge base no longer draws lessons); knowledge.test (`SummaryCard` in `SummaryTab`, no `AiNoteLabel`); notelectures155 (note-only branch moved into `LessonScreen`). New: test/lessonRoute351.test.mjs. 1763 tests pass.

## Review
Reviewer (opus): changes requested (wrong way out «К предметам», head menu toggle/focus) -> fixed; re-review: approve. ux-expert (opus): «доработать» x2 (crumbs/«Назад», opening-tab fallback, disabled accent, duplicate headings; then crumbs from «База знаний», focus after the form) -> fixed; third pass: «ок».
Security: security-auditor not run — view state only, no binding, path or trust boundary touched; model text still rendered as React text.
