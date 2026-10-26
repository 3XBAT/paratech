# TASK-143: «Стоп» and «Повторить» in the agent panel (UI)

Status: done. Frontend only; all acceptance criteria hold.

## Done
- Progress merge (`progress: true`) changes only status and stop reason of its request, clears its old problem; cards stay. The stopped card is ignored while the request is PLANNING/EXECUTING.
- «Стоп»: quiet text button on the waiting line and progress card in PLANNING and EXECUTING; one call (synchronous `PressTracker`, so a double click is one `stop`), disabled while the call runs.
- «Повторить»: round-arrow `IconButton` ("Запустить просьбу заново") under the last request in DONE/FAILED/CANCELLED/STOPPED, hidden when the stop card/problem offers retry or restart. The new request appears at once with the same text (optimistic entry `rerunAsked`, removed by `rerunRefused` / a refusing update). `rerun_blocked` shows «Повтор недоступен: просьба уже изменила данные. Можно отменить действия или задать вопрос заново.»; the button stays (the core re-checks).
- Owner beta bug (25.10): a refused busy request now offers «Стоп» (stops the running request of the same subject) and «Отправить ещё раз» (same text and scope, through normal Ask).
- No text promises a fresh or cached rerun (TASK-142 open).
- Reducer guard: problem-only updates (status "") are no longer dropped for DONE/CANCELLED (needed for `rerun_blocked`).
- History reducer fills an entry still waiting for its first update.

## Decisions to confirm
- «Повторить» shows only under the last entry of the whole panel, not per thread.
- «Повторить» stays visible after `rerun_blocked`.
- Out-of-list file touched: `agent/AgentPanelView.tsx` (passes `last`/`blocked`).

## Follow-ups
- Rerun from a history entry shows an empty request bubble: `UpdateView` carries no request text (needs a binding/core field).
- Low: any problem-only update on turn N removes the pending N+1 optimistic entry (short window, text lost only).
- Panel glue in `AgentPanel.tsx` (`run`, `press`, rerun dispatch) is not covered by tests, only the reducer, view model and tracker.
- Busy card: «Отправить ещё раз» is also disabled while its «Стоп» is pressed.

## Changed existing tests
`test/agent.test.mjs`: progress-card and waiting-card tests expected no «Стоп» in PLANNING; now they expect it (AC).
`test/agentstoprerun.test.mjs`: new.

## Security
security-auditor: no findings. Reviewer: changes requested, then approve with one medium; all fixed (two fix rounds), lows above.
