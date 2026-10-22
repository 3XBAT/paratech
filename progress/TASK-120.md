# TASK-120: agent panel UI

Done: right panel (340px column on every screen) with header input, Ctrl+K, context label (removable -> «Без предмета»), cards (plan, questions, progress, result, reply, cannot-do, action line, stop/problem), offline mode. Reducer keyed by thread/turn, tolerant to reordering; failed Ask (empty threadId) matched by scope; failed button op keeps the known cards and attaches only the problem. Model text only as React text nodes; artifact ref parsed to a label, never opened. Files: `src/agent*`, `src/bindings/agent.ts`, `test/agent.test.mjs` (436 tests pass, build ok).

## Decisions to confirm
- Context label = section name (no subject selection exists yet; subjectId is empty).
- Icon rail from the concept not rebuilt (existing text rail kept).
- Offline stop card offers «Продолжить» and «Отмена».
- Review (opus): High (failed op wiped cards) and 3 Mediums fixed, re-review approve, one more Medium (problem hidden under stop card) fixed. Security auditor: no High/Medium.

## Follow-ups
- Answer card shows only «Ответ с источниками подготовлен.»: the view carries just an answer id; text and source links need the core to send them.
- Confirm card has no buttons (no confirm call in the binding). Undo button disabled until TASK-123; History unavailable.
- Go `TestFrontendCallsBoundNames` (settings_test.go) does not list agent.ts (Go file, out of scope); a TS test covers the names.
- Core: after `abort` in cases/agentturn/step.go the driver publishes the pre-failure state; publish the stored state instead.
- Per-thread version in updates (TASK-119 follow-up). Lows skipped: two Asks sharing a scope may swap texts; refusal notice is global; no unit test for busy/double-click logic in AgentPanel.

## Changed existing tests
Two in `agent.test.mjs` of this task's own first commit were adjusted during review (failed ops use status "" as the driver sends); no pre-existing test changed.
