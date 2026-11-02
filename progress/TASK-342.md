# TASK-342 — remove the paid free-topic work notice

## Done
- Owner 2026-11-02: the notice is useless. A work on a free topic (`triage_assignment{topic}`) now runs at once as the request's one paid job without a plan (the path it already took when the setting was off): triage, budgets, "second write needs a plan" and the key `ag:<thread>:<turn>:<callID>` are unchanged.
- Go: removed `values/freework.go`, `CardFreeWork`, `thread/freework.go`, `Facts.FreeWorkNotice` and the class-UI branch of `Thread.Admit`, `Settings.FreeWorkNotice`, `preferences.SetFreeWorkNotice`, `input/agent/freework.go` (`FreeWorks`), `freeWorkCard`, `Facts.WithNotice`/`NoticeReader`, wails `SetFreeWorkNotice`, `AgentService.FreeWork`/`FreeWorkCard`, the DTO fields, app wiring. A class-UI admission now falls to `read` and is refused as "unavailable".
- Catalog 10 `set_setting` refuses `free_work_notice` (catalog 9 untouched). Hub and `shared/` have no mention: no Hub follow-up.
- Migration `0048_drop_free_work_notice.sql` (drops the column; Down re-adds it as 0032 did; 0032 untouched); `docs/contracts/schema.sql` mirrored; sqlc v1.31.1 regenerated. Test: a DB at 47 with `free_work_notice=0` upgrades, other settings kept, Down/Up works.
- Old data: cards were never stored (live panel events only); an old chat keeps the plain `triage_assignment` call and `{"status":"awaiting_student"}` result, which reads like any result and shows nothing in "earlier requests". Tests: `adapters/sqlite/agent_freework_test.go`, `cases/agentchats` `TestEarlierWithAnOldFreeWorkCall`. Frontend: an unknown card kind (`free_work`) is dropped by `parseUpdate` without an error, test in `agent.test.mjs`; a settings reply with `freeWorkNotice` still parses.
- Frontend: removed `FreeWorkCard`, `freeWorkView`, the Settings toggle and the whole «Помощник» group (it held only the toggle), binding types/methods, CSS.
- Owner addition (02.11): the agent-panel stop button «Продолжить» is «Повторить» for the stopped reasons offline (a streamed reply that broke without a result) and quota; same call (`continueTurn`, same key, free replay). Then no round `rerun` «Повторить» next to it (never two «Повторить»); a stop card next to a `retry_required`/`restart_required` problem drops continue/resume.
- Docs: `docs/agent-parity.md` rows for the card and the setting dropped; ADR-0033 `set_setting` list and "three toggles".

## Decisions to confirm
- «Стоп» by the student keeps «Продолжить» (new ButtonId `resume`, same call) and keeps the rerun («Запустить просьбу заново»): ux-expert found «Повторить» after «Стоп» replays the same answer and takes away the way to a new one. The owner asked for «Повторить» only for the broken stream; the quota stop got it too (same free replay).
- Loss of the default confirmation: an injected planner can now start one paid topic triage per student request without a click (security-auditor: accepted behaviour, Low; bounded by one paid unplanned job per request, the Hub quota, Undo `RemoveWork`; no email/calendar/file effects).
- Migration number 0048 (next free at push time; origin/master had 0047 last).

## Follow-ups
- ux-expert: a free-topic work starts with no explanation that no matching assignment was found (one muted line in the work card: «Задания с такой темой нет — работа пишется по теме.»).
- Quota stop card should say when the limit resets and not offer «Повторить» until then.
- Stop card: caption «Ответ остановлен» and «Остановлено» duplicate each other after a kept typed reply; buttons lack descriptive tooltips/aria; consider «Закрыть» instead of «Отмена».
- Reviewer low: the stop card with reason `failed` + problem `retry_required` (or `expired` + `restart_required`) could still show a doubled button if the shown reason is stale; filter buttons the problem already offers.
- Many Go comments still call `Continue` «Продолжить» (thread/doc.go, agentturn, driver, hubclient); `docs/agent-parity.md` row 173 lists «Продолжить»; `actionpolicy` test name "free topic allowed without the card".
- A failed settings load shows «Не удалось загрузить настройки.» without a button (old).

## Changed existing tests
- Deleted (covered only removed code): `values/freework_test.go` (its `TestRefusalOf` moved to `result_test.go`), `settings/freework_test.go`, `input/agent/freework_test.go`, frontend `agentfreework.test.mjs`, `agentfreeworkcard.test.mjs`, two free-work tests of `followups193.test.mjs`.
- Rewritten: `thread/freework_test.go` -> `freetopic_test.go` (direct run), `input/agent/freework_driver_test.go` -> `freetopic_test.go`, `wails/agentfreework_test.go` (guard that the bindings are gone); new `TestTriageTopicWithoutSubject` replaces `TestFreeWorkWithoutSubject`.
- Edited: `wave10_test.go`, `compensation10_test.go` (free_work_notice refused), `preferences/service_test.go`, `wails/settings_test.go` (7 bound methods), `wails/agent_test.go`, `sqlite/settings_recognition_test.go`, `sqlite/migrate_test.go` (latest 48), `actions_test.go` (comment); frontend `agent.test.mjs`, `agentreplydraft340`, `agentsnapshot`, `agentstoprerun` (rerun now false for offline/quota, true for «Стоп» and budget), `settings.test.mjs`, one-line fixture fields in five tests.

## Security
security-auditor: no High/Medium; only the accepted Low above. Reviewer round 1: medium (doubled «Повторить» with a retry_required problem) and low (lost subject-less test) fixed; ux-expert «доработать» (critical: «Повторить» after «Стоп») fixed; reviewer round 2 on the fixes: approve (one theoretical low, follow-up).
