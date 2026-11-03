# TASK-377: E2E pack «Настройки и подключения»: Hub, key, model, schedule

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-375
- **PRD sections / AC:** ADR-0035 §4, §6; PRD §8.1, §12; AC-07, AC-12, AC-16, AC-23, AC-25
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-377

## Goal
The first pack sets the sandbox up through the real UI and proves the connection, model and schedule features work.

## Allowed to change
- `paratech/e2e/specs/settings/**`, `paratech/e2e/fixtures/{schedule,subjects}/**`, `paratech/e2e/src/fixtures/**`
- `paratech/e2e/scripts/fetch-model.*` (download `ggml-base-q5_1.bin` into `paratech/dist/cache/e2e/`, size and SHA-256
  from `desktop/internal/app/catalog.json`, reused when it matches)

## Out of scope
- Mail and Google sections (not covered in Sprint 1, ADR-0035 consequences).

## Acceptance criteria
- [ ] «Адрес сервера» (Подключения → Дополнительно) = harness URL; the key typed into the key field; the quota line
      shows the E2E limit (AC-12, AC-25). A revoked key (`hub keys revoke` via the harness) shows the rejected state.
- [ ] Model import through «Импортировать модель» + `pick("model", …)`: model listed, checksum verified (AC-23), set as
      default (AC-07); a corrupted copy (one byte changed, made by the fixture helper) is refused with a readable text.
- [ ] The speech component is «готов» (from the exe folder).
- [ ] Synthetic ICS (`fixtures/schedule/week.ics`: subjects «Матанализ», «Литература», «История»; one lesson starting
      2 min after the run start, built at run time; a synthetic call link `https://telemost.yandex.ru/j/00000000000001`
      for «Матанализ» in the event text: it must pass `IsCallHost`, ADR-0032; it is only ever written to `opened.jsonl`)
      imported with preview, then committed (AC-16); the lessons appear on «Сегодня»/«Расписание».
- [ ] No row needs owner data; each feature is one report row with screenshots.
- [ ] Pack ≤ 5 min; `npm test` passes; run log of `--grep @settings` in progress.
