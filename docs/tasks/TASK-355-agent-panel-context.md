# TASK-355: Agent panel — honest context chip, suggestions in the empty state, input at the bottom

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-352, TASK-345
- **PRD sections / AC:** ADR-0034 §2; ADR-0021, ADR-0028; audit А-1.2, 15.1, 15.2, 15.3
- **Агент:** только UI: pure view state (the ids already go in AskInput)
- **Branch:** ralph/<date>-task-355

## Goal
The panel says what it knows about the open screen and shows what can be asked; «Спросить paratech» leaves «Сегодня».

## Allowed to change
- `src/agent/{AgentPanel,AgentPanelView,ChatBar}.tsx`, `src/{agentView,agentState}.ts`, `src/App.tsx` (pass the open
  subject/lesson/assignment), `src/pages/{TodayPage,TodayParts}.tsx` (remove AskSection), `src/todayView.ts`, tests

## Out of scope
- Command palette and changing Ctrl+K (stays the panel shortcut); core changes.

## Acceptance criteria
- [ ] The chip names the open object: «Матанализ», «Матанализ · пара 21.10», «Реферат по истории»; on a screen without
      an object there is no chip; the ×-button reads «Не учитывать» and drops the ids; `AskInput` carries subjectId /
      lessonId / assignmentId of the open object.
- [ ] Empty state: 3–4 suggestion chips by screen; on «Сегодня» they are the core's `screen.chips`; others are fixed
      texts in `agentView.ts` (e.g. on a subject «Что было на прошлой паре?», on an assignment «Сделай под ключ»,
      «Перенеси срок на пятницу»); a chip fills the input, never sends.
- [ ] The input is always at the bottom of the panel.
- [ ] «Сегодня» has no «Спросить paratech» block.
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
