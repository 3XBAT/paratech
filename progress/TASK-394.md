# TASK-394: Hub heartbeat on streamed calls

## Done
- `gateway.proto` (additive): `bool heartbeat = 2` in `CompleteStreamRequest` and `AgentStepStreamRequest`; `message StreamHeartbeat {}`; oneof member `heartbeat = 3` in `CompleteStreamResponse` and `AgentStepStreamResponse`. Comments: transport only, not in the fingerprint or key, never after the result. `buf generate` ran (`shared/gen/**`); buf lint, format and breaking (vs origin/master) clean.
- `hub/internal/input/connect/heartbeat.go`: `DefaultHeartbeatInterval` (15 s), `WithHeartbeatInterval` (GatewayOption, for tests), `heartbeatOf`.
- `gateway.go`: `deltaSender` goroutine is the single writer: deltas and heartbeats go through its `select`; the timer restarts after every message actually sent; a heartbeat is skipped once `finish` started; `finish` waits for the goroutine, then the handler sends the result. `agent.go` passes the same sender. Without the flag there is no timer and the stream is as before.
- The flag is read only in the handler and never reaches the use-case commands, so fingerprint, key, quota and request log cannot depend on it. `hub/internal/app` and config unchanged (default through `NewGateway`, so the scripted E2E Hub with `HUB_PROVIDER_STREAM=true` needs no change).
- Tests (`heartbeat_test.go`, `heartbeat_internal_test.go`; both RPCs): 2.5 intervals of silence give exactly 2 heartbeats, then deltas and result; provider error after a heartbeat gives a Connect error, no result; no flag gives no heartbeat; same key with and without the flag (both orders) is a free replay, no IDEMPOTENCY_CONFLICT, no charge, one provider call; replay has no heartbeat; sends never overlap, nothing after `finish`; a heartbeat Send error stops the sender.

## Decisions to confirm
None. The proto field numbers from the task were free.

## Follow-ups
- Low: a replay normally gets no heartbeat because it answers at once; if the stored-result lookup ever took more than 15 s one could precede it (harmless, the client ignores it). The proto comment says "a replay gets none".
- Low: heartbeat_test.go timing tests use a 200 ms interval with about 100 ms slack; if CI flakes, raise it to 400-500 ms.
- Low, not new: a client that stays connected but stops reading blocks `Send` (deltas already did).
- TASK-395 must set the flag in both desktop streams and reset its watchdog on `StreamHeartbeat`. The Hub must be restarted to get the feature; clients that do not set the flag are unaffected.
- Not run: Postgres-backed hub tests (`HUB_TEST_DATABASE_URL` not set, Docker not started); this change touches nothing in Postgres.

## Changed existing tests
- `hub/internal/input/connect/delta_internal_test.go`: passes the new `heartbeat{}` argument to `newDeltaSender`.
- `shared/contract_test.go` (`wantFields`): records the new fields, oneof members and `StreamHeartbeat {}`; the test requires every contract change to be listed (file outside the listed scope, needed).

## Review
reviewer (opus): approve, three lows above. security-auditor (opus): no findings.
