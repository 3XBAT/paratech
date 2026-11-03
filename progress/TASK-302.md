# TASK-302 — accessibility of the import-recording panel

Done together with TASK-303 (one branch, one PR).

## Done
- One focus mechanism for all three places that open the panel (RecordingsPage, SubjectPairsTab, KnowledgeBasePage): `useDisclosureFocus(open)` + the pure rule `focusMove` (`src/disclosureFocus.ts`). Opening moves focus to the first enabled control of the panel; closing returns it to the opener only if focus was lost or is still on the opener. It acts only when `open` changes.
- Opener: `aria-expanded` always, `aria-controls` only while the panel exists (panel gets the id).
- RecordingsPage: the empty-state focus effect no longer fights the hook (it skips when focus is already inside its target).
- Mode switch (`Segmented` got an optional `disabled`) is off while an import runs; so are the other fields of the form (`disabled`, quiet style `.rec-import-panel .input:disabled`).
- `.rec-start { display: flex }` fixes the real mismatch: the block wrapper made the start button a few pixels taller than «Импортировать запись» in the head.
- UX audit fixes: the busy primary button «Выбрать файл и импортировать» uses `aria-disabled` instead of native `disabled` (focus is not lost when the native dialog closes); the opener is disabled while importing on all three pages with a visible reason (`Button.disabledReason`, text moved to `importTexts.importBusy`); «Загрузка…» has `role="status"`; shared `firstControlSelector`.

## Decisions to confirm
- Escape does not close the panel (date/time pickers use Escape themselves); «Закрыть» is reachable by Tab.
- Text `pairsTexts.importBusy` moved to `importTexts.importBusy` (same wording).

## Follow-ups (lows)
- On «Пары» the busy reason is shown twice in one row (opener and «Добавить конспект прошедшей пары»).
- Some tests check the source with regexes (no DOM in the harness); real focus is only covered by the owner's steps.
- `aria-busy` on the busy primary button; `firstControlSelector` does not skip `aria-disabled` controls.
- If re-reading lessons fails after an import in «Пары», the whole tab is replaced by the error line (panel and result lost).

## Changed existing tests
`test/subjectTabs353.test.mjs`: the busy-reason text now comes from `importTexts` and the opener expects `disabledReason` instead of `title` (behaviour unchanged).

## Security
Frontend only, no new inputs or bindings; no security review needed beyond the shared audit of the round (no findings).
