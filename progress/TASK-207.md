# TASK-207 — agent chats UI (ADR-0028)

## Done
- `agent/ChatBar.tsx` (new): header with the open chat title, chat list menu (unarchived by activity, collapsed «Архив» with aria-expanded), «Новый чат», rename in place (Enter/Esc, 1–80 runes), «В архив» / «Вернуть», keyboard (arrows, Home/End, Esc, Tab closes), aria-live notes.
- `AgentPanel.tsx`: chat list/open chat state, Ask passes `threadId`, chip untouched, updates routed per chat by `threadId`, running mark, chat problem texts (`busy` has its own text, `not_found` re-reads the list), earlier requests of the open chat via `chatEarlier` as text (student text, mention pills with stored label, reply line, no buttons).
- `agentView.ts`: view-model (sections, running, visibleEntries, earlierItems, reconcile, problem texts, title check). `journalView.ts` + `pages/JournalParts.tsx`: chat title in the journal line meta (plain text).
- Review fixes: a failed Ask in a named chat arrives as turn 0 / status "" and is now matched to its pending local request (Stop/resend kept); `blockingEntry` per chat; open chat no longer jumps; unnamed requests stay visible; rename focus; menu a11y.
- Tests: `test/agentchatsui.test.mjs` (20 tests). No existing tests changed.

## Files outside the task list
`src/agentState.ts` (update routing by threadId, failed-Ask match, blockingEntry), `src/pages/JournalParts.tsx` (one line), `src/agent/ChatBar.tsx` is under `agent/*`.

## Decisions to confirm
- Earlier requests: text only (ADR Open 7 not answered). Requests seen live in this session keep their full cards.
- A request of the open chat that is `current` but not seen live (after a restart) shows as text, with no «Да»/«Продолжить»/«Стоп»: no binding returns the current request's state. Needs a snapshot binding (follow-up) or owner's OK.
- Tab inside the open menu closes it and returns focus to the toggle (not to the next control).

## Follow-ups (lows)
- Snapshot binding for the current request of a chat opened after a restart (reviewer medium).
- Pure helpers + tests for refreshChats choice and auto-open gate; orphan key should include threadId; rapid double Ask in one chat can swap a busy refusal with the first request's update (carry a local request id).
- Ring TASK-208, restart-from-here TASK-249, @-input TASK-247 not here.

## Security
Auditor: no High/Medium; all untrusted texts are text nodes. Reviewer: first round changes requested (1 high, 1 medium), fixed; re-review approve.
