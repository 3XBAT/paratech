# TASK-396: automatic retry of a failed agent step, Hub failure = failed, Retry also from offline

## Done
- `domain/jobs/services/backoff/interactive.go`: `Interactive(attempt) (time.Duration, bool)`, attempt = the retry about to happen (1..3): 5 s, 10 s, 20 s, then `false`; `InteractiveRetries = 3`. Table test.
- `domain/agent/values`: `ErrHubFailure` ("the Hub answered"). `adapters/hubclient/errors.go` adds it to every error that carries a Hub ErrorDetail (provider error, refusal, unusable answer, rate limit, quota, unknown code); transport, timeout and `unavailable` without a detail do not carry it. Error class and text unchanged. Documented on `AgentModel` (`cases/agentturn/ports.go`).
- `cases/agentturn` (`step.go`, `service.go`): a transient Hub-call failure is retried up to 3 times under the SAME key after `backoff.Interactive`. Never retried: quota, permanent failure, `ErrInvalidResult`, `ErrTurnGone`, Hub not configured, a call cut by «Стоп», a caller that went away. The wait releases the service lock (Ask, Stop, other chats do not wait); a per-thread waiter registry lets `Stop` and `Cancel` end the wait at once (stored STOPPED/user, or CANCELLED); the stored version is checked before and after the wait. A thread read failure during the wait aborts the request (stopped offline), never leaves it PLANNING. After the retries: error with `ErrHubFailure` -> `StopFailed`, else `StopOffline` (quota first).
- `Progress` port gets `Retrying(ctx, st, attempt)` (publish-only, once when the wait starts and again when the retried call goes out). `input/agent`: `Update.Retry`; `input/wails`: `AgentUpdate.Retry` and JSON `retry` (int, 0 = none, always sent); `app/agent.go` wires the Sleeper and copies `Retry`.
- `domain/agent/thread`: `Retry` allowed from `offline` too (key `:r<N>`, `MaxRetries` shared by failed and offline). `Continue` with the same key only after «Стоп» (user) and quota; an open step stopped offline/failed returns `ErrRetryRequired`. Exception kept: offline with NO open step (calls waiting for the Driver) still continues (there is no key to change, otherwise a dead end).
- Frontend: `bindings/agent.ts` `UpdateView.retry` with a tolerant parse (positive integer else 0). UI line is TASK-399.
- Package docs and comments that said «Продолжить» replays after offline were rewritten to the new rule.

## Decisions to confirm
1. **Sleeper instead of Clock.** The AC says the wait is "через Clock". The `Clock` port has no Sleep and the app's local clock / many test clocks would all need it, so a new optional narrow port `agentturn.Sleeper` (as the job runner does) is wired to the system clock in `app`. Without it there is no automatic retry (existing environments unchanged). Time still comes only through ports.
2. **Offline card is two clicks until TASK-399.** `agentView.ts` stopTexts.offline still offers `continue`; it now returns `retry_required` and the panel then offers «Повторить». TASK-399 must switch the offline card to `retry`. A blind switch now is unsafe because of the offline-without-open-step exception (Retry refuses there), so the card needs the step state.
3. **A stream that ends without a result (or a watchdog cut) after the first message has no ErrorDetail**, so it stops `offline` («нет связи»), not `failed`, although the Hub was reached. The AC defines the mark strictly by ErrorDetail. Suggested for TASK-398/399: mark `ErrHubFailure` in `hubclient/agent.go` once any message was seen. Owner/supervisor decision; outside this task's files.

## Follow-ups (low)
- Money: when the app closes/crashes during a Hub call (Interrupt/Recover), or the local save fails after the Hub answered, the request is stored offline with its step open and no automatic retry used the old key; «Повторить» now sends a new key and pays again, while a result the Hub stored under the old key is not reused (ADR addendum item 2 accepts the cost, but its reason does not hold on these paths). Hardening: a domain flag that lets Continue reuse the key for such stops.
- `answer_question` auto-retry is NOT here: `cases/answering` belongs to TASK-397.
- `Retry-After` of a rate limit is ignored (fixed 5/10/20 s); maybe `max(Interactive, RetryAfter)`.
- Stop quickly followed by Continue can make the first caller see a version conflict with a running state (cosmetic).
- `ErrHubFailure` lives in `domain/agent/values`; if TASK-397 needs it, move it to `domain/shared`.
- `app/agent.go` and `desktop/tests` (5 files) are outside the listed files (two-line wiring; tests that asserted Continue after offline).
- `go test -race ./desktop/internal/app/...` takes ~9 min (not caused by this task).
- E2E agent pack not run here; D3/D4/D5 may stay red until TASK-399.

## Changed existing tests
- thread: `TestStepsAndKeys`, `TestRetryKeys` (same-key case uses user/quota stop; offline mixed into the retry budget); `calls_test`.
- agentturn: `TestContinueAndRetryKeys`, `TestAttemptsOfANewRequestEndWithItsText` (offline case goes through Retry), `TestFailedSaveAfterStepContinues` -> `TestFailedSaveAfterStepRetries`; fake Progress gained `Retrying`; `stuck_test`, `progress_test`, `service_test`.
- wails exact-JSON expectations gained `"retry":0` (agent, agentcurrent, agentfill, agentsummary).
- desktop/tests: `agent_actions`, `agent_catalog`, `agent_driver` (`TestDriverOfflineStopsAndRetries`: `retry_required`, then Retry, key `:r1`), `agent_retention_sqlite`, `agent_sqlite` (`TestAgentKeysOnAutoRetryRetryAndRestart`: a lost answer is retried automatically under the same key and replayed free). None weakened; asserts are stricter.

## Security
security-auditor (opus): no High/Medium; two lows recorded above (money after crash/save failure; Cancel did not wake the wait, fixed). reviewer (opus): approve with changes; two mediums (read failure in `pause`, stale docs) fixed in 7446d6d6, ambiguity of a cut stream recorded as decision 3.
