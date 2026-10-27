# TASK-205 — agent chats use case (ADR-0028)

## Done
- `cases/agentchats` (new): List (unarchived by last activity, then archive, with running), New (returns the empty current chat; mutex), Rename / Archive / Restore (load, aggregate method, save, 3 retries on version conflict), Earlier (read-only: student text without the upload line, mentions with labels, reply line from shown planner cards with Russian captions, no tool results; current request marked `Current`).
- `cases/agentturn.Ask`: `AskInput.ThreadID` (empty = current chat) and `Mentions` (<= 8). Each mention is checked through the optional `Mentioned` port before the lock; missing ones are dropped, a failing check fails the Ask; no port = no mention kept. Mention IDs join provenance; a request in an archived chat restores it; the first text sets the auto-title (via `thread.Ask`). Hub contract unchanged.
- `app/mentions.go`: `mentionObjects` over library (subject, material not in the bin), schedule (lesson not deleted), homeworkflow (assignment not deleted); wired into agentturn. agentchats is NOT wired in app yet (see below).

## Existing tests changed
None.

## Decisions to confirm
- Rename while a request runs returns ErrBusy (otherwise a rename between a step's saves makes agentturn drop the reply). TASK-207 must show it.
- Earlier includes the current request, flagged `Current`; no "без ответа" text when no card was shown (Reply empty).
- Mention existence: subject = in library list (archived included); removed material, deleted lesson/assignment do not exist.

## Follow-ups
- BLOCKER for wiring: `agentchats.ChatRepository.RequestMentions(ctx, id, turn)` has no implementation in adapters/sqlite (~20 lines in adapters/sqlite/agent.go next to RequestScope, GetAgentTurn + mentionWire, ErrNotFound; plus a test). Outside this task's files; do it in TASK-206 (which wires app) and then `agentchats.New(Deps{Chats: sqlite.NewAgentThreadRepository(db), History: sqlite.NewAgentHistory(db, clock), Clock, IDs})`.
- Reviewer medium (domain scope): replyLine/cardLine in agentchats/earlier.go duplicate thread/reply.go rules (export e.g. `thread.ShownCards`); studentText in earlier.go undoes the upload line of values.NewStudentText (add the inverse next to it). Domain task.
- Reviewer medium: Rename/Archive/Restore do not share agentturn's lock; a save between Ask's load and first save gives ErrVersionConflict and the message is lost with an error. Retry load+Ask once in agentturn, or share a lock in app.
- Low: Earlier reads <= 1024 rows (long chats lose oldest requests silently) and calls RequestMentions per request (N+1); Ask checks the 8-mention cap before de-duplication (aggregate dedups first); app/mentions_test failing-store case lacks lesson and assignment; Service.New and Ask(empty) can make two empty chats.
- input/agent/driver.go:202 passes `IdleInput{}` for a stuck request; with a named chat pass `IdleInput{Thread: in.ThreadID}` (TASK-206). input/wails/agent.go:164 passes no ThreadID/Mentions yet.
- Rerun/Again reuse stored mentions without re-checking existence (no exploit: executors fail with not-found).
- TASK-207 renders Earlier texts as plain text only.
- ADR-0028 Open 1-7 untouched. Earlier assumes Open 2 (b: label as at mention time) and Open 3 (a: dropped hidden) by default only.

## Security
Auditor: no findings. Reviewer: approve, no blockers; mediums above are follow-ups.
