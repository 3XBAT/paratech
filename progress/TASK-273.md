# TASK-273 — live card of a chat's current request after a restart (ADR-0028)

## Done
- `agentState.ts`: new `snapshot` action, `loadSnapshot`, `hasLiveRequest`, `snapshotText`. On opening a chat the panel calls `chatCurrent`; an update (not null) becomes an entry under the same key as a live update (`thread:<id>:<turn>`), so later updates replace it; text of the request comes from `chatEarlier` (same turn).
- A live request of the chat (update, rerun, request just sent) wins: the snapshot is not even asked for, and a late one is dropped by the reducer. A snapshot arriving after a chat switch is dropped.
- null / DONE / FAILED / CANCELLED: nothing changes, the history text line stays. Problems of the call show as a Russian line (`chatProblemText(..., "current")`); `not_found` re-reads the list.
- TASK-207 nit: the orphan key is `orphan:<threadId>:<scope>`.
- Cards are the existing ones (plan «Да», questions, «Продолжить», «Стоп»); model text stays plain text.
- Tests: `test/agentsnapshot.test.mjs` (confirm buttons after restart, «Да» calls approvePlan with thread and turn, stopped/running, no duplicate on update, live wins, null, other chat, problems, orphan key).

## Decisions to confirm
- A propose_* confirm card of an ended request is not restored (TASK-272 open question); owner check uses a plan waiting for «Да».
- tasks.json: TASK-272's PR set TASK-273 to `done` by mistake; this PR sets TASK-272 to `done` and keeps TASK-273 `done`.

## Follow-ups (lows from review)
- A request sent into the chat before chatCurrent returns blocks the snapshot; if the core then refuses it as busy, the running request shows as a card only after its next update. Fix: re-load the snapshot when a send into the open chat is refused.
- The snapshot waits for chatEarlier (student's text); if it never settles, no card. A short timeout or dispatching with empty text would remove it.
- AgentPanel wiring of loadSnapshot has no DOM test (no harness); pure functions are covered, owner check covers the rest.
- Orphans (threadId "") show in every open chat; with the per-thread key two chats' failures show as two orphans.

## Existing tests changed
None.

## Security
Auditor: no findings. Reviewer: approve (lows above).
