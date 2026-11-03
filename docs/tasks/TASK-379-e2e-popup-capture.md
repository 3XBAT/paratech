# TASK-379: E2E pack «Окно-напоминание и запись звука»

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-377
- **PRD sections / AC:** ADR-0035 §2, §4; ADR-0007; PRD §6.1; AC-01, AC-03, AC-24; TASK-370
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-379

## Goal
The popup is driven as a second window: it appears at the synthetic lesson start, its buttons work, and (opt-in) a
real capture of the default device ends as a transcribed recording.

## Allowed to change
- `paratech/e2e/specs/popup/**`, `paratech/e2e/src/sound/**` (plays a fixture on the default device with
  `System.Media.SoundPlayer` through PowerShell; WAV only)

## Out of scope
- Focus-steal measurement (stays in the human checklist of ADR-0007 / TASK-020).

## Acceptance criteria
- [ ] The popup page becomes visible (`document.visibilityState`, or the Wails event the page uses) no later than
      20 s after the lesson start of the TASK-377 schedule (AC-01); screenshot of the popup.
- [ ] «Подключиться» (TASK-370) appends exactly the synthetic link to `opened.jsonl`, becomes disabled, popup stays.
- [ ] «Не записывать» hides the popup; a second synthetic lesson offers it again; «Начать запись» starts a recording
      shown in the busy pill / «Записи»; «Остановить» on the main window stores it (AC-03).
- [ ] With `--with-sound` only (env `E2E_WITH_SOUND=1`): the clip is played during the recording; the transcript is
      not empty (AC-24, whole-computer fallback on 19045). Without it the row is «не проверено (нужен звук)».
- [ ] Pack ≤ 5 min; `npm test` passes; run log in progress.

## Notes
If the hidden popup is not a CDP target until first shown (TASK-375 refs), the spec attaches when it appears.
