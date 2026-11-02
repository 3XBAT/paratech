# TASK-362: One entry «Сделать задание» on the assignment page

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-346; owner answer В2 of ADR-0034 (this draft follows the recommended «а»)
- **PRD sections / AC:** ADR-0034 §2, §7; ADR-0025; PRD §10; audit А-6.6, 7.4, 7.5
- **Агент:** только UI: pure view state (the agent's turnkey tools are unchanged)
- **Branch:** ralph/<date>-task-362

## Goal
The student sees one way to get an assignment done, with two clear modes and their cost.

## Allowed to change
- `src/pages/{HomeworkPage,HomeworkTurnkey}.tsx`, `src/{homeworkView,homeworkTurnkey,turnkeyView}.ts`,
  `src/agent/{WorkCard,AgentCards}.tsx` (compact card only), `src/styles.css`, tests

## Out of scope
- Core and Hub changes; prices (the cost line uses what the bindings already give).

## Acceptance criteria
- [ ] One block «Сделать задание» with a choice: «Короткий ответ (текстом, ~1 мин)» → today's draft flow; «Работа под
      ключ (файл Word или PowerPoint, с вопросами, дороже)» → today's turnkey flow; each choice shows that it spends
      minutes of processing before the click.
- [ ] After a draft or a work exists, the block shows it and its actions; «Версии» and «Письма» are folded sections
      at the end with counts.
- [ ] The work card in the agent panel is compact: status + «Открыть в задании»; it is managed only on the page.
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.

## Notes
If the owner answers В2 = б or в, this task is rewritten before it starts.
