# TASK-147 — answer draft to the panel (core)

## Done
- `input/agent`: `Sink.Draft(Draft)`, `Draft{Thread, Turn, CallID, Texts, End}`. The Driver gives `Answerer.Ask` a `callDraft` bound to (thread, turn, call) and to the call's ctx (the one «Стоп» cancels). It drops drafts after the ctx ends or after `end`, skips empty/repeated ones, strips all Unicode Cf characters (bidi, zero-width, BOM, soft hyphen). Draft text never reaches results, history, journal or log.
- `input/wails/agentdraft.go`: `agent:draft {threadId, turn, callId, texts}` at most 10/s per call, last value wins, timer on a fake-able clock. An `answered` end or a running-request update flushes waiting texts before the card update; a stopped/failed update or a `dropped` end discards them and blocks later drafts of that turn until the request runs again.
- `cases/answering`: `drafter` rescans the partial answer at most every 100 ms or on doubling, plus one final scan after a successful read (fixes the O(n^2) follow-up of TASK-146).
- `frontend/src/bindings/agent.ts`: `draftEvent`, `DraftView`, `parseDraft`. `app/agent.go` maps the sink.

## Decisions to confirm
- `Draft` has an extra `End` field ("" / answered / dropped) so the panel can tell a successful read (flush before the card) from a failed one.
- Out-of-list files: `cases/answering/service.go` + `service_test.go` (the throttle must live where `answer.Draft` is called), `desktop/tests/agent_driver_test.go` (no-op `Draft` on `driverSink`), `frontend/test/agent.test.mjs` (one `parseDraft` test).
- Cf stripping removes ZWJ too (breaks emoji sequences in drafts only).

## Changed existing tests
- `chanSink` (input/agent fakes) and `driverSink` (tests/) gained the `Draft` method; `TestPanelSinkMapsUpdates` got extra draft-mapping checks. No assertion changed.

## Follow-ups
- TASK-148 (reviewer M2): a failed read emits no event, so the frontend must clear drafts of a request (thread, turn) on ANY non-progress, non-problem update of it, not only on the card or the stop (matches `draftsBefore`). Render as text nodes only, marked «черновик», never use `callId` in selectors/HTML.
- Reviewer L1: output within 100 ms of the last scan waits for the next delta or the final scan (about 200 ms lag with the panel throttle).
- Reviewer L2: no real-clock `-race` test of `time.AfterFunc` against a concurrent `Publish`.
- TASK-146 gap still open: call timeout firing mid-stream.
- `AgentPanel.drafts` keeps one tiny entry per thread for the process lifetime.

## Security
Auditor: no findings. Reviewer: approve (M1 scope, listed above; M2 follow-up for TASK-148).
