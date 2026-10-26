# TASK-148 — progressive answer draft in the panel (UI)

## Done
- `agentState.ts`: `PanelState.drafts`, action `draft`, `draftsFor(state, entry)`. A draft is kept per (thread, turn, call), last value wins. Ignored: empty thread, turn older than the thread's newest, request already in a non-running status (DONE/CANCELLED/STOPPED/FAILED). A newer draft turn or any newer update turn drops older drafts of the thread. Any update of the request that is not progress and not problem-only (status "" with no cards) clears its drafts (card, stop, end status, failed read).
- `AgentCards.tsx`: `AnswerDraft`, caption «Ответ пишется · черновик», muted `<p>` text nodes only, dots after the last line (the waiting card's, else the last draft's). `callId` is never in markup.
- `AgentPanel.tsx` subscribes to `agent:draft` via `parseDraft`; `AgentPanelView.tsx` follows the draft only when the thread was at the bottom (24 px); reduced motion is covered by the global `scroll-behavior: auto`.
- Tests: `test/agentdraftrender.test.mjs` (attach, replace by card, clearing matrix, stale, late draft, no HTML injection, dots).

## Decisions to confirm
- Files outside the list: `AgentPanelView.tsx` (scroll logic lives there), `test/homework.test.mjs`.
- Existing test changed: TASK-102 «no user-facing text says «черновик»» now strips the exact caption literal from `agentView.ts` first; the ban stays elsewhere. Product wording conflict between TASK-102 and this AC: confirm.
- The waiting card now renders after the other cards (drafts sit before it), so with a problem card the dots/«Стоп» come after it.
- Go `draftsBefore` keeps drafts on any problem update; the frontend clears them on STOPPED+problem (safer).

## Follow-ups
- Drafts for a thread no request knows are kept without a cap until reload (auditor: resource use only).
- No automated scroll-follow test (logic is in the view).

## Security
Auditor: no findings. Reviewer: approve; medium (late draft after STOPPED/FAILED) and two lows fixed, no re-review (small fix).
