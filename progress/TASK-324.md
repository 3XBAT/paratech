# TASK-324 — agent UI: wave-10 cards in the panel (result lines, delete_assignment, schedule_subjects, offer_button); not reachable from the model until TASK-326

## Done
- Go (view mapping, `input/wails/agentpanel.go`): `cardView` maps `DeleteAssignmentCard` (target kind `assignment`, TASK-320 follow-up); `ActionLine` now sends `offer` (TASK-322 follow-up) and an additive `result` (`values.LineResult`, `domain/agent/values/wave10lines.go`): kinds marked_done/undone (count 1..50), note_text (excerpt: <= 3 lines x 120 runes, cleaned of control/format characters), note_upload/move/rename/remove, recording_started/stopped, setting (allowlist enums). Filled in `input/agent` (`lines.go` + one-line hooks). The excerpt exists only in the live card: not in intent, journal, log, error or planner result; a replayed call has no line. A zero value = today's generic line (old event bytes unchanged).
- Frontend: result lines («Отмечено выполненным: N», «Записано в «Мой конспект»: …», «Запись началась · Остановить», «Запись остановлена · Начать запись», settings lines); `delete_assignment` card («Удалить «…» и событие в Google Календаре?», «Не удалять» / «Да, удалить»); `schedule_subjects` card reusing the «Предметы» review rows (`Создать N предметов`, `Отменить всё`, `Вернуть` for a removed subject, stale/partial/undone/declined with «Открыть «Предметы»»); `offer_button` card and the offer on action lines with a handler per action (`src/agentOffer.ts`); unknown card kind -> honest stub «Эту часть ответа нельзя показать в этой версии paratech. Обновите приложение.»; journal labels and the recording-stop reason.
- Offer mapping: open_material/open_summary/open_lecture_text/open_transcript/save_as(open work file)/google_sync call the page's own binding; google_sign_in and open_help open «Подключения»; download_model «Модели речи»; sound_check «Диагностика»; start_recording opens «Записи» with the start panel; undo_action undoes the chat's latest undoable action and shows which; open_call_link/copy_call_link render nothing (TASK-261/328).

## Decisions to confirm
1. Glossary beats the task text: «Мой конспект» (not «конспект»), the page's own wording for schedule results («Создано предметов: N», not «Заведено N»), «Не создавать» (not «Нет»), «Да, удалить» / «Не удалять» on the delete card (not bare «Да»/«Нет»), «Повторить» on a half-done schedule card, «Задания» / «расшифровка» (word-ban test).
2. Offers that navigate (google_sign_in, open_help, download_model, sound_check, start_recording) do not call "the same binding" of ADR-0033 §3: the page's button needs the page's fields/dialogs. They are labelled by what happens («Открыть «Подключения»»).
3. ac_power_only line says «Обработка только от розетки …» (the settings page's word); recognition_load says «Нагрузка при расшифровке: …».
4. Part A copied `node_modules` from `paratech-team2` into this worktree (untracked, not committed).
5. After a restart an applied schedule card reads stale (TASK-323 decision 2) — the card points to «Предметы».

## Follow-ups
- Go-fixed refusal texts still say «нажмите «Да»» (`ErrAgentConfirmInUse`, `ErrAgentConfirmUnavailable`, `ErrAgentConfirmCalendar`, `ErrAgentConfirmScheduleName`, `msgSchedulePartial`) while the delete button reads «Да, удалить» / the schedule button «Создать N предметов»; change Go and `errorTexts` together (the frontend compares strings for equality).
- The delete card has no «Вернуть» (an agent deletion is not restorable from the assignments page, whose «Вернуть» lives only for deletions made on the page): restoring is asked from the chat (`change_assignment{restore}`); decide whether the card should offer it.
- Nice-to-have from the UX audit not done: «Открыть» / object names on note, marked-done and setting lines; «Открыть запись» after a stop; truncating the review rows in a narrow panel («Показать все (N)»); focus/highlight of the page's button after a navigating offer; journal label of recording by outcome.
- `marked_undone` wording and «Отменено: <line>» capitalisation of the undo_action success line are mine; glossary pass advised.
- TASK-320 Low: «Нет» on the delete card is not stored (re-read shows waiting) — Go side, unchanged.

## Changed existing tests
- Go: `notes_test.go` (`noSecret` now allows the excerpt only on the line and also checks journal rows/intents; `TestNoteActionsAndUndo` expects the line kind/excerpt), `settings_test.go`, `recording_test.go` (exact line now includes `Result`), `assignment_done_test.go` (stricter: exact count of cards, count 1 for the deleted case).
- Frontend: `agent.test.mjs` and `agentchatcurrent.test.mjs` (an unknown card kind is now kept as the "unknown" stub instead of dropped); `agentconfirm.test.mjs` (loading text «Загрузка…», no «Нужно подтверждение» while loading). Expectations of this task's own new test files were adjusted after review (wording only; none weakened).

## Security
Auditor: no findings (excerpt only in the live card, text nodes only, closed offer enum, Go provenance checks, card targets from the core's reply). Reviewer: round 1 approve-with-changes; UX expert «доработать» with 12 must-fix items, all applied in a83b5b39 except those listed in follow-ups; re-review found one Medium (the post-delete button opened the deleted assignment), fixed in the last commit (opens the «Задания» list).
