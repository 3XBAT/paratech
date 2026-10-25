# TASK-141: «Повторить» a finished agent request (core)

Status: done. All acceptance criteria hold, with one stricter-than-written rule (see decisions).

## Done
- `thread.Rerun(live, now)`: allowed on the current request in DONE/FAILED/CANCELLED/STOPPED. Order of checks: status (PLANNING/EXECUTING -> `ErrTurnInProgress`, others -> `ErrInvalidTransition`), then `live` (`ErrRerunBlocked`), then time. A refusal changes nothing. A stopped request is cancelled and its open calls closed not done; the new request (next turn, empty budget) has the same scope and text; first key `ag:<thread>:<turn+1>:1`.
- `agentturn.Effects{Live(ctx, thread, turn)}` port (optional in `Deps`; without it Rerun fails `ErrInvalidConfig`); `agentturn.Rerun` loads the current turn (second click -> stale), reads `Live` (a read error is a failure, never "no effects"), re-adds the stored text (with uploads) and drives the new request.
- `agentjournal.Live`: true when any entry of (thread, turn) is not FAILED/UNDONE. Reads `ListByThread` (200 newest; enough for the newest request, no new sqlite query). `app/agent.go` builds the journal before `agentturn.New` and passes it as `Effects`.
- `Driver.Rerun` (same marks as Continue/Retry: busy while a stopped loop still finishes an action), problem `rerun_blocked`; `AgentService.Rerun(ref)` (refused `offline` like Ask), allow-list updated; `rerun` in `frontend/src/bindings/agent.ts`.
- Pinned: the planner history of a re-run ends with the re-run's own text (`TestRerunHistoryEndsWithItsText`). The known `thread.Window`/Hub merge bug is not touched. The answer key is not touched (TASK-142).

## Decisions to confirm
- A journal proposal entry stays `PROPOSED` forever (terminal), so any request that ever proposed something can never be repeated, even after the student declined it. The AC says "open proposal"; letting declined ones through needs the proposal state behind the `Effects` port. Safe direction; matches ADR-0026 open question 2.
- `AgentService.Rerun` is refused `offline` when the Hub is not set up (not in the ADR).

## Follow-ups
- TASK-143 UI: «Повторить» -> `api.rerun({threadId, turn})` on the last request when DONE/FAILED/CANCELLED/STOPPED, never together with Retry's «Повторить»; Russian texts for `rerun_blocked` (offer «Отменить всё»), `busy`, `stale`, offline; new `ButtonId` in `agentView.ts`.
- `input/wails/agentpanel.go` line 67 doc comment does not list `rerun_blocked` (file outside the task).
- Low: `Rerun` reads `Live`/text before the domain status check (a journal read error on a running request reports failed instead of busy); `Live` relies on the 200-entry window; `thread.Rerun` leaves a partial change if `StartRequest` failed after `Cancel` (unreachable); a proposal whose journal write failed has no entry, so a rerun can propose again (still needs the student's click); tiny window before `inflight` registration (same as Continue/Retry).
- No end-to-end sqlite test in `desktop/tests` (outside allowed files).

## Changed existing tests
Only additions: `Rerun` on test fakes (input/agent, wails, agentturn `fakeEffects`), rows in `TestDriverPublishesProblems`, `TestAgentForwardsOnTheAppContext`, malformed refs, `TestAgentOffline`, three assertions in `frontend/test/agent.test.mjs`.

## Security
security-auditor: no findings. Reviewer: approve, one medium (the proposal rule, recorded above), lows listed.
