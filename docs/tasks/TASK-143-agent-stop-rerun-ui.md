# TASK-143: «Стоп» and «Повторить» in the agent panel (UI)

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-139, TASK-140, TASK-141
- **PRD sections / AC:** ADR-0021, ADR-0026 «Стоп» and «Повторить»
- **Branch:** ralph/<date>-task-143

## Goal
The student can stop a running request from the waiting line and repeat the last finished request.

## Allowed to change
- `paratech/desktop/frontend/src/agentView.ts`, `agentState.ts` (+ tests)
- `paratech/desktop/frontend/src/agent/{AgentPanel.tsx,AgentCards.tsx,agent.css}` (+ tests)
- `paratech/desktop/frontend/src/bindings/agent.ts` only if TASK-140/141 left a gap

## Out of scope
- Go code, drafts (TASK-148).

## Acceptance criteria
- [ ] An update with `progress: true` changes only status and stop reason of its request; its cards stay.
- [ ] While `PLANNING` or `EXECUTING` the waiting line has a quiet «Стоп» (text button, keyboard reachable); it calls
      `stop(ref)` once (disabled while the call runs); the stop card «Остановлено · Продолжить · Отмена» follows.
- [ ] The last request of the thread in `DONE`, `FAILED`, `CANCELLED` or `STOPPED` shows «Повторить» (round arrow,
      title «Запустить просьбу заново»), except when its stop card already offers «Повторить» (failed step → `retry`)
      or «Начать заново» (expired). It calls `rerun(ref)`; the new request appears below as a new entry with the same
      text.
- [ ] Problem `rerun_blocked`: «Повтор недоступен: просьба уже изменила данные. Можно отменить действия или задать
      вопрос заново.»; `stale` and `busy` use the existing texts.
- [ ] Older requests show no «Повторить»; texts impersonal, in Russian.
- [ ] Tests: progress merge keeps cards; Стоп shown only while running; Повторить rules per status and stop reason;
      double click sends one call. `npm run test` and `npm run build` pass.
