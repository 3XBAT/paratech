# TASK-270 — «Уплотнение контекста…» and the ring after compaction

## Done
- Per-chat page state (`CompactingMap`, `compactingFromUpdate/FromList/Of` in `agentView.ts`): a compaction update with a chat id sets/clears the flag; an empty threadId (Ask without a chat) shows nothing; any real update of the chat clears it too (a lost end update cannot leave the note on); the chat-list read prunes gone chats. Compaction updates still never reach `reducePanel`.
- `CompactionNote` (ContextRing.tsx): one muted line «Уплотнение контекста…» in an always-rendered `role=status aria-live=polite` container (zero height when idle), under the field form, in normal flow (no fixed/absolute/sticky, no box, no tint, no animation). The field stays enabled.
- While compacting, `ContextHint` («Начать новый чат?») is not drawn; the note takes its slot (same CSS order/margins) and the thread is re-pinned to the bottom when it toggles (UX review).
- Ring: redraws from the end update's fill (new, lower percent/level; the full hint goes away); a failed compaction (fill null) keeps the ring.
- Overflow without compaction (expired-request restart): ring text «Начало разговора агент уже не помнит: новый чат ответит точнее.» and the hint line «Начать новый чат? Начало этого разговора агент уже не помнит; чат останется в списке.» (`ringTexts.overflow/overflowHint`, `RingModel.hintText`).
- Tests: `test/agentCompaction270.test.mjs` (new). README of `src/agent` updated.

## Decisions to confirm
1. Placement: a quiet line UNDER the field (same slot as the full-chat hint), not a banner «над полем ввода» as the AC says — owner's quiet-UI rules (no banner for small loading).
2. Text-only, no dots: the thread already shows the Waiting dots for the same request (one indicator per operation).
3. ADR-0028 «The ring» says Overflow happens only when a compaction failed; TASK-269 showed it also appears after an expired-request restart, so the overflow wording is neutral and the ADR sentence is now outdated (not edited).
4. A late update of an older request in the same chat hides the note early (fails toward hide, never stuck).

## Follow-ups
- UX: after a successful compaction the ring says «Агент помнит весь разговор.» though the start is only a summary; needs a `compacted` flag in `ContextFill` from the core.
- UX: the text «Уплотнение контекста…» is an internal term (owner's literal wording); «Сжатие начала разговора…» would match the other ring texts — ask the owner.
- UX: a compaction shorter than ~1 s makes the line flash; consider a 600 ms show delay.
- UX: overflow way out is only «Новый чат» (guidelines suggest «с кратким итогом»).
- Low: `ContextHint` remounts with content, so a screen reader may not announce it; keep its container always mounted.
- Wiring of `AgentPanel` (event handler, list prune) is covered by pure-helper tests only.

## Changed existing tests
`agentcontextring.test.mjs` (overflow text asserted; 90-char cap only for `overflowHint`, others 70); `agentreplydraft340.test.mjs` line 223 (source-pin regex gained `props.compacting` in the scroll effect deps). No test weakened or skipped.

## Security
Frontend only: fixed texts and a fill number, no model text reaches the page; security-auditor not run (no trust boundary touched). Reviewer: approve (CSS-guard medium fixed); ux-expert «доработать» (4 items) fixed; re-review approve.
