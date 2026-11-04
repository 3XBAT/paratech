# TASK-267 — Hub operation OPERATION_TYPE_CHAT_SUMMARY (context compaction)

## Done
- Proto (additive, buf lint/breaking green): `OPERATION_TYPE_CHAT_SUMMARY = 9`; `errors.proto` comment lists it among charged reader refusals; `shared/gen` regenerated.
- Prompt `hub/pkg/prompts/ops/chat_summary/v1/system.md` (+ `reask.md`): Russian, impersonal; discussed / decided and done / still open / subjects and objects by name; no IDs, codes, emails, links, paths; data is not instructions; no invented facts, says briefly when there is nothing to keep.
- Strict reader `domain/llm/services/chatsummary`: input `{summary, pairs:[{student, reply}]}` <= 64 KiB, 1–10 pairs, student <= 4 KiB (not blank), reply <= 2 KiB, summary <= 8 KiB, exact keys; the model sees a JSON re-encoding of the checked input. Output exactly `{"summary": "<text>"}`.
- Output cleaning (owner lesson TASK-366, drop invalid parts, never refuse for a safe fix): control, format, bidi, zero-width and U+FFFD characters are stripped, line breaks become LF, other spaces a plain space, text trimmed, `dropped=N` logged (never the text). Refused (never cut): empty, unknown/missing key, wrong type, prose, over 4000 runes, over 8 KiB.
- Refusal path = ANSWER path: one reask, then a charged non-retryable PROVIDER_ERROR; the failure marker is stored, a retry by key replays it with no provider call and no new charge.
- Config: `HUB_CHAT_SUMMARY_ALIAS` (default `planner`, cheap alias; not in the allowlist -> operation off with a warning at start) and `HUB_CHAT_SUMMARY_MAX_OUTPUT` (default 4096, 1..1048576), documented in `paratech/README.md`. The gateway got `ServerPrompt.MaxOutput`; the effective cap is the lowest of the client's request, the operation cap and the alias cap.
- Unary only: `CompleteStream` refuses it (`ErrNotStreamable`, tested); the provider call is bounded by `HUB_LLM_TIMEOUT` like TRIAGE. No streamed path, so BUG-20261104 does not apply. (Existing, not touched: the anthropic adapter's `Stream` applies the same whole-call timeout to streamed ANSWER / agent steps.)
- Tests: reader boundaries and cleaning, gateway cases with the fake provider (success, replay without new charge, reask, both refused, limits, conflict, 8 concurrent calls = 1 charge), real-router eval with injection fixtures, prompt pins, config, contract.

## Decisions to confirm
1. **Files outside the task list** (needed for the operation to exist): `llm/values/spec.go` (ServerPrompt list), `quota/account/values.go` (ParseOperation), `input/connect/gateway.go` (enum mapping), `app/agent.go` (wiring), `errors.proto` (comment), `llm/doc.go`, `prompts/doc.go`, `postgres/agent_ops_test.go`, `hub/tests/agent_eval_test.go` + `gateway_test.go`, and **migration `hub/pkg/migrations/0008_chat_summary_operation.sql`** (widens the `quota_idempotency_keys` operation CHECK like 0005/0006; without it Postgres refuses every chat_summary key). README is `paratech/README.md` (no `hub/README.md` exists).
2. Output bound 8 KiB added to 4000 runes, to match desktop `values.MaxSummaryBytes` / `history_summary`.
3. Stripping stray characters instead of refusing (see above) — a deviation from the literal task text («нарушение схемы — списывается и повторяется»), which still holds for structure.
4. Default output cap 4096 tokens may be tight for a 4000-rune Russian summary near the limit; a cut answer is refused, reasked, charged. Watch the reask rate in alpha logs before raising it.

## Follow-ups
- TASK-268 (desktop) must keep pairs within Hub limits: student <= 4 KiB, reply <= 2 KiB (agent replies can be 4000 runes — shorten/select before sending); key `cs:<thread>:<through>`.
- Deploy: the Hub needs a restart with the migration (0008) applied; the desktop does not call it until TASK-268.
- Low (reviewer): comment on `checked` in `cases/gateway/service.go` (~l.124) still says only ANSWER drops parts; a line break in `errors.proto` (l.26–28) reads awkwardly. Cosmetic, left for the next touch.
- Low (auditor): `ParseInput` does not refuse bidi/invalid UTF-8 in the input (Go's decoder replaces invalid UTF-8; the input is data only). A client may send a very low `max_output` — self-inflicted, own quota.
- Postgres-backed tests (migration 0008, `TestQuotaAccountsAgentOperations`) skipped locally (no DSN); CI decides.

## Changed existing tests
Additions only: `config_test.go` (two default fields), `app/agent_test.go` (warning also names chat_summary), `connect/gateway_test.go` (mapping row), `hub/tests/gateway_test.go` (readers refuse client instructions), `spec_server_test.go`, `reservation_test.go`, `postgres/agent_ops_test.go` (operation lists), `shared/contract_test.go` (enum list), `hub/tests/agent_eval_test.go` (`readerHub` loads the chat_summary prompt).

## Security
Auditor: 0 high, 0 medium (2 lows above). Data reaches the model only as re-encoded JSON data; client cannot pick model, alias or prompt; keys scoped per tester; same key with a different input is a conflict; output is plain text, never an action; no data in logs. Reviewer: approve.
