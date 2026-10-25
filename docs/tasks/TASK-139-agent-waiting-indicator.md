# TASK-139: Agent panel waiting indicator

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** -
- **PRD sections / AC:** ADR-0021 (cards, impersonal voice), ADR-0026 «Waiting», ADR-0012 (visual direction)
- **Branch:** ralph/<date>-task-139

## Goal
Replace the bare «Отправка…» line and the job-less «Работа идёт» card with three softly pulsing dots while a request
is sent or runs, like other chat assistants.

## Allowed to change
- `paratech/desktop/frontend/src/agentView.ts` (+ its test)
- `paratech/desktop/frontend/src/agent/AgentCards.tsx`, `paratech/desktop/frontend/src/agent/agent.css`
- new `paratech/desktop/frontend/src/agent/Waiting.tsx` (+ test) if useful

## Out of scope
- Any Go code, bindings, «Стоп» placement (TASK-143), drafts (TASK-148).

## Acceptance criteria
- [ ] While the call runs (`update === null`, no problem) and while the status is `PLANNING` or `EXECUTING` without a
      job progress card, the entry shows one waiting element: three dots, accent colour (`--accent-fill`), opacity
      `breath` animation of `paratech-ops/design/concept-agent.html` staggered per dot; no visible word.
- [ ] The element has `role="status"` and a visually hidden text «Запрос выполняется»; screen readers do not repeat it
      on every update.
- [ ] `prefers-reduced-motion: reduce`: dots static (no animation), still visible.
- [ ] Job progress cards (`progress` with a job) and their «Стоп» keep working as before; refusal texts unchanged.
- [ ] Dots disappear on the first card that ends the wait (plan, questions, reply, stop, problem, done).
- [ ] Frontend tests for the view model (when the indicator shows and hides); `npm run test` and `npm run build` pass.

## Notes
Owner rules: UI texts in Russian, impersonal voice, one accent colour. The existing `.ag-dot` in `agent.css` may be
reused. The core is unchanged; ARCH-44 (no business rules in UI): the decision is only status → display.
