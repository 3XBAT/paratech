# TASK-382: E2E LIVE set before each beta

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-376, TASK-378, TASK-379, TASK-380, TASK-381
- **PRD sections / AC:** ADR-0035 §1, §4, §6; ADR-0023; AC-11, AC-12
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-382

## Goal
`npm run e2e:live` runs a small tagged set against the real provider through the local Hub, spends at most the run cap,
and tells whether the model and prompts behave before a beta.

## Allowed to change
- `paratech/e2e/{package.json,playwright.config.ts}` (project `live`), `paratech/e2e/src/live/**`
- `@live` tags and live rubrics in `paratech/e2e/specs/{recording,agent,settings}/**`
- `docs/process/e2e.md`, `docs/process/supervision.md` (one step: «перед бетой: scripted + live, отчёт в PR беты»)

## Out of scope
- New scenarios beyond the set below; turnkey work.

## Acceptance criteria
- [ ] Live mode uses TASK-374 live harness; missing `PARATECH_E2E_ANTHROPIC_KEY` = one environment row «нет ключа»,
      no run. `HUB_QUOTA_LIMIT` default 400000, override `E2E_LIVE_TOKENS`.
- [ ] Set: settings (key, quota), recording chain with a real summary and extraction (summary non-empty, ≥ 1
      assignment or an explicit «заданий нет»), the five agent scenarios of TASK-380 with rubrics: titles of the
      synthetic recordings appear in the reply; the link question ends at the link or a screen with «Подключиться»;
      the lesson answer cites the lesson; the subject is added and undoable; after the induced tool failure the reply
      contains none of the deny-list causes («нет интернета», «сервер недоступен», «ошибка сети», «нет доступа») unless
      present in the tool result.
- [ ] Retries 1 per scenario; a pass on retry is «готово после повтора». Report adds tokens used (quota before/after
      via `GetRemaining`) and the run time; LIVE ≤ 30 min.
- [ ] One real live run on the dev machine, its report summary in progress (tokens spent stated).
- [ ] `npm test` passes; no provider key in any file under `paratech/dist/e2e-run/` (test greps).
