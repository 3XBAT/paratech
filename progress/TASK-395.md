# TASK-395: hubclient, silence watchdog for answer_question and the heartbeat flag

## Done
- `adapters/hubclient/stream_watch.go` (new): one shared watchdog, `watchStream(ctx, first, idle, total)` (the former inline timer of `streamStep`), `watchTimeout` (a cut becomes `unavailable(ErrTimeout)` with the cause `errStepSilent` / `errStepTooLong` as detail). Used by `Step` and `AnswerQuestion`; nothing duplicated.
- `AnswerQuestion`: cut after 60 s (`StepIdleTimeout`) without a Hub message, up to `RequestTimeout` (2 min) before the first one, 10 min overall (`DefaultAnswerTimeout`, now 10 min; `Config.AnswerTimeout` stays as the overall cap, the 5-minute whole-read cap is gone). Caller cancel stays the ctx error. The unary `Complete` fallback has its own `RequestTimeout` context, as the unary step.
- Both streams (`CompleteStream`, `AgentStepStream`) send `heartbeat = true`. A `StreamHeartbeat` or an event without a known oneof member only resets the watchdog (no delta, no partial, no result). A Hub without heartbeat behaves as before.
- Config: negative values refused; an explicit `StepIdleTimeout` above `AnswerTimeout` is refused; the http backstop covers the longest cap. Comments in `client.go`, `doc.go`, `errors.go` updated; `errStepTooLong` text now says "stream".
- Tests: `answer_timeout_test.go` (heartbeats keep a silent stream alive, partials only from deltas, total silence cut, older Hub silent / writing long, overall cap, first-message window, caller cancel, unary cap, config), `TestStepStreamHeartbeat`, error cases "heartbeat after the result" and "only heartbeats". Durations scaled down (idle 300 ms).

## Decisions to confirm
None. Decisions taken: `AnswerTimeout` kept as the overall cap instead of folding into `StepTimeout`.

## Follow-ups (low, from review)
- A heartbeat after the result is a permanent `ErrBadResponse`, like any message after the result (the Hub never sends one, ADR-0026). Deliberate strictness, pinned by tests.
- The unary fallback of an older Hub without `CompleteStream` is now bounded by `RequestTimeout` (2 min), not the old 5 min, as the task says ("как у Step").
- `agent.go`, `doc.go`, `errors.go` and the new `stream_watch.go` are outside the task's file glob (`step*.go`, `config*.go` do not exist; Step lives in `agent.go`); doc/error files are comment-only.
- Names `StepIdleTimeout`, `errStepSilent`, `errStepTooLong` now serve the answer stream too; a rename can wait.
- Not run: `go test -race` of `app` (importer; vetted under GOOS=windows only; it does not use `AnswerTimeout`).

## Changed existing tests
- `TestAnswerQuestionHasItsOwnTimeout` (answer_test.go) removed: it asserted the 5-minute whole-read cap and default that the task removes; its checks are covered by the new tests (long answer, transient timeout, defaults, backstop, negative value).
- `TestAnswerQuestionStreams`, `TestStepStreamsTheReplyDraft`: the expected stream request now includes `Heartbeat: true`.

## Security
security-auditor (opus): no findings (heartbeats only re-arm the idle timer; the overall cap and the http backstop still bound the read; no Hub text in timeout details; key handling unchanged). reviewer (opus): approve, four lows above.
