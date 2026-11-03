# TASK-378: E2E pack «Запись → конспект → задание» with real whisper-cli (closes TASK-025)

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-377
- **PRD sections / AC:** ADR-0035 §4, §6; ADR-0009 (recording → STT → post-processing); PRD §8; AC-06, AC-08, AC-09;
  TASK-025
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-378

## Goal
A synthetic lecture goes the whole real way: import, local whisper-cli, transcript, scripted summary and extraction,
lesson screen and assignment; this run is the evidence TASK-025 waited for.

## Allowed to change
- `paratech/e2e/specs/recording/**`, `paratech/e2e/scripts/llm/recording/**` (scripted rules)
- `paratech/e2e/fixtures/audio/**` (one public-domain Russian clip ≤ 60 s, WAV or OGG, ≤ 2 MB, with `README.md`:
  source URL, reader, licence, expected words) — if owner answer О3 differs, follow it

## Out of scope
- Live summaries (TASK-382); capture from the device (TASK-379).

## Acceptance criteria
- [ ] «Импортировать запись» on «Записи» + `pick("recording", ["audio/<clip>"])`, linked to «Литература»: the row goes
      through its statuses to «Конспект готов» without a restart; the STT job ran the real whisper-cli of the exe folder
      with the default model (AC-08: the model id is shown or in the log).
- [ ] Lesson page «Расшифровка» contains at least 2 of the expected words of the fixture README (case-insensitive).
- [ ] «Конспект» shows the scripted summary; the scripted extraction creates an assignment with a due date that
      appears in «Задания» and «Сроки на неделе» on «Сегодня».
- [ ] AC-06: no recorded Hub→provider request carries audio (no `RIFF`/`OggS`, no base64 run > 4 KiB); asserted on
      `llm.requests()`.
- [ ] Cancel: a second import, its STT job cancelled from «Фоновая обработка» → the row shows the cancelled state, no
      transcript, no whisper-cli process left (`tasklist`).
- [ ] Restart: app stopped hard during a third STT job, started again on the same sandbox → the job finishes (AC-09).
- [ ] The report has the STT speed line (audio seconds / wall seconds, model, CPU name) for information.
- [ ] Pack ≤ 5 min on the dev machine; `npm test` passes; progress lists the run and says TASK-025 may be set done by
      the supervisor (this task does not edit TASK-025).
