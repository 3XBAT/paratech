# TASK-393: «Отменить расшифровку» and «Расшифровать снова» on the recording row (UI)

## Done
- Cancel icon (`IconAction` "x", danger, tooltip and aria-label «Отменить расшифровку») at the end of the progress line (status cell) of a row with `canCancel` in stage transcribing/queued; busy while the call runs. It is raised above the stretched `.row-open` overlay (`.st .ia` in styles.css): the first E2E run showed the overlay swallowed the click.
- First cancel asks «Отменить расшифровку?» (names the recording and the cost; «Да, отменить» danger / «Не отменять», Esc = «Не отменять», checkbox «Больше не спрашивать»). The choice is a local UI flag (localStorage `paratech.confirm.cancelTranscription.skip`, try/catch, null-safe), stored only with «Да, отменить».
- Cancelled row: quiet status «Расшифровка отменена» (neutral, icon "stop", hint «Запись сохранена.») + main word button «Расшифровать снова». Failed transcription (TASK-409): old text + the same button. This fixes the TASK-409 gap (a FAILED row had no action).
- Results: cancel cancelled/not_running = quiet refresh, ending = row note inviting another try, failed = row note; retranscribe started = refresh, not_allowed = quiet refresh (TASK-392 follow-up), no_model = «Модель расшифровки не установлена.» + «Открыть модели речи», failed = row note. A rejected call becomes the row note, not the page banner. Ref-based in-flight guard: one call on a double click.
- A row cancelled in this session stays in «Идут» (page-state `kept` set, pruned once the row was seen offering retranscribe and no longer does); `isFinishedRow` unchanged, so old failed rows fold. Focus moves to «Расшифровать снова» after a cancel (bounded: 10 s / 4 refreshes), back to the icon after a failed cancel.
- «Сегодня»: `problemOf` shows a cancelled row calmly («Расшифровка отменена») instead of «Проблема с записью».
- E2E `specs/recording/02a-retranscribe.spec.ts` (icon, dialog «Не отменять», «Да, отменить» with the box, cancelled row, second cancel without dialog, third run to «Конспект готов», 3 Hub calls); `02-cancel` (delete path) kept; README stale line fixed.
- docs/agent-parity.md: one row (no tool yet; follow-up `cancel_transcription` / `retranscribe`).

## Decisions to confirm
1. Icon in the status cell, not in the actions cell (the grid has fixed columns and the main button is usually present).
2. Main button priority: stop, runNow, retranscribe, lesson, open (so «Расшифровать снова» is not buried in «⋯»; texts stay in «⋯»).
3. Cancelled rows stay in «Идут» for the session only (kept set), then fold into «Завершённые» with the button.
4. `todayView.ts`, `src/styles.css`, `src/pages/RecordingsPage.tsx` and the new `src/transcriptionCancelView.ts` were touched beyond the task file list (needed for the wiring).

## Follow-ups
- UX must-fix deferred (needs Settings): no way to bring the question back after «Больше не спрашивать»; add a «Подтверждения» group in «Настройки» (switch «Спрашивать перед отменой расшифровки»), the same key; later flags (calendar, email) go there.
- Cancel as an item in the «⋯» menu; a queued row shows only the icon (no progress); the dialog closes silently when the recognition finishes meanwhile (UX lows).
- Agent tools `cancel_transcription` / `retranscribe`.
- The cancelled row leaves «Идут» after an app restart (by design).

## Changed existing tests
- `recordingrow.test.mjs`: action list gained `retranscribe`. `list.test.mjs`: assertion for retranscribe rows (kept as `isFinishedRow` true). `iconbuttons368b.test.mjs`: inventory of word buttons of RecordingRow. `lessonEntry352.test.mjs`: selector list of raised controls gained `.st .ia`. `e2e/test/recording.test.ts`: file list and feature count (02a).

## Verification
`npm run test` (2310 pass) and `run build`; `npm --prefix paratech/e2e test` (445 pass); real E2E (settings + recording projects through heavy.sh, `--no-build` after `build-alpha.ps1 -E2E`): 18 passed, including 02a (49.8 s), 02-cancel, 03-restart, 04-privacy. A first run found the overlay bug (fixed); a Postgres start-up flake (environment) was repeated.

## Review
reviewer (opus): changes requested twice (E2E toggle check, race, isFinishedRow scope, kept-set flicker, focus bounds) - all fixed; UX expert: wording, dialog cost, thrown-call note, focus fixed, Settings switch deferred. Security: no new input over Wails; the only new storage is a non-secret UI flag; the recording title in the dialog is rendered as text.
