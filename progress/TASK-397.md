# TASK-397: answer_question key per call, interactive retry 5/10/20 s

## Done
- `input/agent/answer.go`: `callScope(prefix, callID)` = `ag:<thread>:<turn>:<8 hex of sha256(call id)>`, computed per call in `answerQuestion`; `answerScope(st)` stays and now gives the prefix (so `tools.go` is untouched). Fits `answer.MaxScopeBytes` (64): about 49-59 bytes for app thread ids; `MaxScopeBytes` unchanged, key stays `ans:v2:`, no migration. «Продолжить» replays the step with the same call ids, so the same key (free replay); a second `answer_question` of the request has another call id, so a new paid attempt.
- `cases/answering`: `Ask` (one read, used by tickets via `app/tickets.go`) and new `AskNow` share one `ask(..., retry bool)`. `AskNow` retries a transient `ErrNoConnection` (ctx alive, not `ErrServiceNotConfigured`, not a permanent class by `backoff.Classify`) up to 3 times under the SAME key after `backoff.Interactive` (5/10/20 s) through the new optional port `Sleeper` (nil: no retry). Each attempt first looks the key up in the store. ctx cancel in the wait returns `ctx.Err()` at once. Quota, refusal, unusable answer, invalid, local store errors: no retry. A new `drafter` per attempt, so the retried draft is passed again from the start.
- `input/agent`: `Answerer` port method is `AskNow`. `app/core.go`: `Sleeper: e.clock` in `answering.Deps`. Tool result error codes unchanged.

## Decisions to confirm
1. **Sleeper, not Clock** (as TASK-396): `Clock` has no Sleep, so a narrow optional `answering.Sleeper`, wired to the system clock.
2. **Draft on retry**: no empty `Draft(nil)` reset (callDraft ignores empty drafts); the panel replaces a call's draft with the newest one, so it starts over with the retried text. During the wait the last draft of the failed read stays shown.
3. **Worst case**: each attempt may run to `AnswerTimeout` (10 min), so up to about 40 min for one call; «Стоп» cancels at once.

## Follow-ups (low)
- ADR item 1 says the panel shows «Повторная попытка N из 3» for `answer_question` too; `AskNow` has no progress hook. Add an optional `Retrying(attempt)` callback in TASK-399 (frontend line) if wanted.
- Call ids are unique only within a step; a provider that reuses an id (`call_0`) across steps would give a second identical question in the same turn the same key (a replayed refusal). Mix the step number into the hash if it shows up.
- Stale comment of `reuse` in `input/agent/tools.go:96` (now only the prefix; file outside the task list) and "one agent request" in `desktop/tests/answering_sqlite_test.go:41`.
- A test in `app` that fails if `tickets.go` switches to `AskNow`; the "tickets do not retry" test sits in `cases/answering`.
- `Retry-After` of a rate limit is ignored (as TASK-396).
- Not run: `go test -race ./desktop/internal/app/...` (about 9 min); `desktop/tests` filtered to Arch/Answer/Ticket/Draft/Driver passed.

## Changed existing tests
- `input/agent/answer_test.go` `TestAnswerQuestion`: scope asserts are per call (`ag:th1:3:d0f631ca` for the same step and call id twice; another call id and another turn differ), pinned hex; fakes `fakeAnswerer`/`streamAnswerer` renamed `Ask` to `AskNow`. `draft_test.go`: the same rename.

## Security
security-auditor (opus): no High/Medium; the key keeps thread and turn in the prefix and is a full sha256 over question, fragments and scope, so no cross-thread collision; model-chosen call ids are hashed, new keys are intended paid attempts under the Hub quota; retry bounded by backoff and cancellable; logs hold no text. reviewer (opus): approve, lows only (listed above); no multiplication with the agentturn step retry (tool errors become `RefuseFailed` results, retry wraps only the planner call).
