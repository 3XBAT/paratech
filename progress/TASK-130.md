# TASK-130 — ДЗ под ключ · UI

## Done
- Work card kind `work` now renders: questions card (choices/text, answer or skip with defaults, "Уточнение N из 2", answered block), «Как понято задание» card with editing (edit -> save -> confirm, work-type select, 200-rune limit), assumptions from a dropped third round, helper mode (steps, «Что сдать», «Типичные ошибки», «Спросить по шагу»), triage/failed cards.
- New: `src/turnkeyView.ts`, `src/agent/TurnkeyCards.tsx`, `src/agent/turnkey.css`, `test/turnkey.test.mjs`. Changed: `src/bindings/turnkey.ts`, `src/agent/WorkCard.tsx`, `src/workView.ts`, `src/main.tsx` (one css import), `tsconfig.test.json`.
- Model text is rendered as React text nodes only (no HTML, no links); a test greps sources for banned sinks.
- npm test 593/593, build green.

## Decisions to confirm
- Helper mode is a card in the agent panel, not a separate page (a page would need nav.ts/App.tsx edits clashing with TASK-134). Confirm acceptable.
- A stored `triage` work is treated as "call due": always shows «Повторить», is not polled (the core does not persist problem/message). Same-version reads keep a reply's problem.

## Unmet / follow-ups
- AskStep has no backend (answers «Вопросы по шагу пока недоступны.», shown as a calm note): the «Спросить по шагу» criterion is only met on the UI side. Needs a core/Hub task.
- Core could persist "call due/in flight" state so the UI can tell them apart (Go follow-up).
- Review lows: editor test "Реферат selected" assertion is weak (OR of alternatives); Retry on an out-of-date view shows a generic error (re-read with api.work on failure); Retry may overlap a triage still running elsewhere (Hub idempotency under concurrent same-key requests to confirm).
- No component-level click tests (no jsdom); interactions are tested via pure functions.
- Visual match with concept-agent.html not checked on screen.

## Changed existing tests
None.

## Security
Auditor: no findings (plain-text rendering, input limits, no side effects without click).
