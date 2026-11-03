# TASK-381: E2E pack «Задания»

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-377
- **PRD sections / AC:** ADR-0035 §6; ADR-0029, ADR-0034 §2; PRD §10; AC-13, AC-14
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-381

## Goal
The student's own assignment flows work end to end in the real window.

## Allowed to change
- `paratech/e2e/specs/assignments/**`, `paratech/e2e/scripts/llm/assignments/**`

## Out of scope
- Sending to the teacher (no SMTP fake in Sprint 1); turnkey files (expensive; later live task).

## Acceptance criteria
- [ ] «Добавить задание» for «История» with text and due date → listed in the right group of «Задания» and in «Сроки
      на неделе».
- [ ] Edit the due date → the group changes; the change is in «Журнал».
- [ ] The short-answer path of the assignment page (today's «Быстрый черновик» or «Сделать задание → Короткий ответ»
      after TASK-362) with a scripted DRAFT answer → draft shown, edited by typing, regenerated with an instruction
      (AC-13); «Сохранить в файл» writes `<e2e-dir>/saved/…` (save hook).
- [ ] «Выполнено» moves it out of the open groups; delete through «⋯» with its confirmation removes it.
- [ ] The send button, if present, stops at the confirmation (AC-14) and the scenario cancels there.
- [ ] Pack ≤ 5 min; `npm test` passes; run log in progress.
