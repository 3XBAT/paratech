# TASK-208 — context ring by the agent input (ADR-0028)

## Done
- `agent/ContextRing.tsx` (new): `ContextRing` (svg ring left of the send button, `role="img"`, `aria-label` + `title` «Контекст чата: N %. <sentence>», visually hidden copy inside the img) and `ContextHint` (only at `full`/overflow, only with `onNew`: «Начать новый чат? Этот чат почти заполнен и останется в списке.» + button «Новый чат», `role="status"`).
- `agentView.ts` («The context ring»): `ringTexts`, `ringModel` (null for null fill and 0 %; level only from `fill.level`, overflow = full, percent floored/clamped), `fillsFromList` / `fillsFromUpdate` / `fillsFromChat` / `fillOf` (one fill per chat). No 70/90 in TS (a source-scan test guards it).
- `AgentPanel.tsx`: `fills` state fed by the chat list, `agent:update` (only a non-null fill changes it) and the replies of new/rename/archive/restore. The ring holds its last value while the core says `running` or the chat's last local request is not final (DONE/STOPPED/FAILED/CANCELLED), so it never moves during a step or while a plan/«Да» waits.
- `agent.css`: ring and hint; transition removed under `prefers-reduced-motion`; `forced-colors` handled; theme tokens only, no red.
- Tests: `test/agentcontextring.test.mjs` (new; only tests of this task were edited in the fix round). No pre-existing test changed. QA: `docs/qa/tasks/TASK-208.md`.

## Decisions to confirm
- The label stays «Контекст чата: N %» (acceptance criterion). The UX expert proposed «Память чата» (the chip's «контекст» means the attached subject/lesson, two words for two things). Owner to choose.
- The ring is hidden at 0 % (empty chat) to keep the empty state quiet; the criteria did not say.
- Sentences: normal «Агент помнит весь разговор.», high «Скоро агент начнёт сжимать начало разговора.» (compaction condenses, not forgets), full «Память чата почти заполнена: новый чат ответит точнее.»

## Follow-ups
- UX expert (must-fix by its list, deferred as design scope): make the ring a `<button>` (≥ 24 px) opening a small flyout (percent, sentence, «Новый чат»), so keyboard users and `high` (70–89 %) get an action; title tooltip is mouse-only. Needs an owner/design OK.
- Reviewer minors: a rerun shown but without an update does not hold the ring (`requestOpen`, treat `update === null` as open); a problem-only update with status "" holds it until the next update; `role="status"` is mounted already filled (many screen readers skip that: keep an always-mounted empty wrapper); nbsp assertion in the test reads opaque.
- UX lows: minimal visible arc for 1–4 % and a stronger track colour (contrast 3:1); a dev seed with a long history for the owner's check; «начать новый чат с кратким итогом» with TASK-269/270.
- TASK-269/270: the post-compaction fill arrives through any update with a non-null fill; the ring redraws from it.
- Merge: TASK-247 edits the same form in `AgentPanelView.tsx`; this task adds one `<ContextRing>` line before the send button and one `<ContextHint>` line after the form.

## Security
Auditor: none found. Reviewer: first round changes requested (2 medium, fixed), re-review approve. UX expert: verdict «доработать», items 1, 2, 5, 6 fixed, 3/4 deferred (above).
