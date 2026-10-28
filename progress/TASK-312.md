# TASK-312 — subjects from the schedule: button and review panel

## Done
- `src/scheduleSubjectsView.ts` (pure, no React): selection, counters, name checks, `buildChoices` (member choices only for doubtful members, as `applyGroup` needs), a reducer with phases idle / saving / undoing / refreshing, batches per token (undo newest first), `createFlow` / `undoFlow` / `refreshFlow` that never throw, texts.
- `src/pages/ScheduleSubjectsReview.tsx`: `ScheduleSubjectsReview` (controller, reusable by the agent card of TASK-324: props `{reply, subjects: {id,name,aliases}[], api, onChanged, onClose(batches)}`), `ReviewView`, `ScheduleOfferLine`, `EmptySubjectsActions`, `KeptUndoLine`, `ScheduleCheckFailedLine`.
- «Предметы»: empty state: the schedule button is the primary action when the proposal has groups, «Импортировать расписание» when the check succeeded with no groups, «Не удалось проверить расписание · Повторить» when it failed. With subjects: one quiet line above the list (the header gets no fifth button); «Предмет» is secondary while the panel is open. The «Отменить всё» line survives closing the panel until the student leaves the page or presses «Закрыть».
- «Расписание» (SchedulePage + App.tsx, outside the task's file list, asked by the supervisor): after an import a hint «Из расписания можно создать предметы: N» shows only if there are groups; its button opens «Предметы» with the panel already open (one-shot flag in App, dropped when the route leaves the list).

## Decisions to confirm
- The entry button keeps the task's wording «Завести предметы из расписания», the main button «Создать N предметов»; inside the panel the verb is «создать».
- `Undo` goes through all batches of the page (several tokens after «Обновить список») newest first; the page keeps them in memory; after an app restart the core answers `invalid` and the panel says undo is no longer possible.
- `attempt` heuristic: after a failed create, subjects named `sched-…` (the core's derived-ID prefix) with a name from the last attempt are left out of the "name exists" check, so an idempotent retry is not blocked. The core stays the judge (reviewer low: it can weaken the hint after a failed attempt; a tighter version records the subject ids at the moment of saving).
- «Импортировать расписание» appears only in the empty state: an empty proposal cannot tell "no schedule" from "all created" when subjects exist.
- Header text `texts.addSubject` = «Предмет» (libraryView.ts) left as is (TASK-339 territory; audit 3.3 suggests «Добавить предмет»).

## Follow-ups
- Low: `pendingReview` (failed check then a later successful load) can open the panel on any later successful load, not only after «Повторить»; move the open decision into a pure helper and test it.
- Low: the failed-check line should be a mounted live region to be announced reliably.
- Delay the «Проверяется расписание…» status by ~1 s (Fluent: no indicator under 1 s).
- «Журнал» entry with undo for the created subjects (ux-expert).
- Merge with TASK-339: both edit the `SubjectList` props and the block around `InboxList` in SubjectsPage; after the merge `onChanged` / the kept-undo line should call 339's `refresh` rather than `load`.
- Esc/focus effects and the `scheduleSubjects()` call of SchedulePage are not covered by tests (no DOM in the runner).

## Changed existing tests
None. New: `test/scheduleSubjects312.test.mjs` (1227 tests in the suite pass, build ok).

## Security
Auditor: no High/Medium (schedule text only in text nodes / input values / fixed aria templates; keys in Maps; undo sends only the token). Reviewer: two highs and several mediums in round one (retry stuck after a partial create; undo order), ux-expert must-fix items (discoverability hint, dead ends, glossary, accessibility); all fixed in two rounds; re-review of the last commit: approve.
