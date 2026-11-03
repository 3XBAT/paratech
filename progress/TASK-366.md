# TASK-366 — большой ответ по материалам возвращается; сдержанное сообщение о сбое (round with TASK-369)

One branch / one PR with TASK-369 (owner allowed). See also `progress/TASK-369.md`.

## Done
- Hub `domain/llm/services/answer`: `MaxClaims`, `MaxClaimRunes`, `MaxCites`, `MaxQuoteRunes`, `MaxMissing`, `MaxMissingRunes` removed; the size is bounded only by the model's output cap. A claim or cite that fails the checks (unknown keys/types, `chunk_id` not sent, quote not in its own fragment, control characters) is dropped (`Answer.Dropped`, logged as a count); the whole answer is refused only when nothing valid remains. Quote matching in new `quote.go` copies the desktop rule (without the 8-character minimum, so the Hub is never stricter); each quote is searched only in its own fragment, so the work stays linear.
- Answer prompt `ops/answer/v2` (+ `reask.md`); v1 kept. The limits phrases are gone.
- Hub gateway: reask (at most one) also for streamed ANSWER calls. The reask is one unary call inside the same reservation; it sends no chunks (a stream shows only the first draft, the final result is the checked answer); both usages charged once, a second refusal stored as `FailedOutput`, replay free.
- Panel v9 (in place, TASK-364 lines kept): "when a tool fails" line — «Произошла ошибка, ответ не получен. Повторить?», no invented cause, no «материала нет», no paid retry on its own.
- Desktop `domain/coursework/answer` parses the answer a second time and had the same limits: removed, same per-part drop; `MaxOutputTokens` 4096 -> 16384 (the Hub clamps to the alias cap, 8192 for `default`: `hub/config/config.go:53`, `modelallowlist.Resolve`). Not in the task's file list but required for the owner decision.
- Desktop log: `hubclient.ErrModelAnswer` (Hub message `invalid_answer` matched by a fixed text, pinned by a Hub test) is kept next to `ErrProvider`, so `desktop.log` tells "provider did not answer" from "answer failed the schema"; the user/model text stays generic (`{"error":"failed"}`). It also maps to `answer.ErrInvalidAnswer` (a ticket is recorded as refused, not retried). An `IDEMPOTENCY_CONFLICT` on the answer operation is likewise a refused answer (see below).
- Answer call has its own timeout (5 min; the shared HTTP backstop follows), because a 8192-token stream plus a reask can pass 2 min.
- agenteval: two live cases f01 (failed `answer_question`: restrained reply, banned cause phrases) and f02 («добавь предмет по последней записи»); case format got `results`, `avoid`, `subject_name`; `MaxRequests` 60 -> 70. A scripted refusal is sent as the real Driver sends it (`IsError`, compact JSON).
- Owner addition (2026-11-03, TASK-364 follow-up): a call link typed without a scheme («zoom.us/j/123», «example.com») gets `https://` in front in `subject.ParseCallLink` (every path alike); `http://`/`https://` stays as typed; other schemes and ports are still refused (ADR-0032 forbids ports: `host:8080/x` becomes `https://host:8080/x` and is refused for the port). Hint texts in `callLinkView.ts` changed (placeholder «zoom.us/j/123 или https://…»). The Zoom password in the link goes to the agent/LLM as is (owner confirmed, no stripping).

## Decisions to confirm
- Idempotency key of the answer stays `ans:v2:`; the new `MaxOutputTokens` and answer prompt v2 change the Hub fingerprint, so an exact repeat of a question whose Hub record is still alive (24 h) gets `IDEMPOTENCY_CONFLICT`; mapped to a refused answer (no migration: `answers.request_key` CHECK allows only `ans:v[12]`). Stored local answers are unaffected. A reworded question works.
- A stored Hub refusal is replayed free for 24 h under the same key: the agent's «Повторить?» after an `invalid_answer` refusal of the same question cannot succeed in that window (refusals are now rarer). Options: fresh attempt suffix on explicit retry, or accept.
- `reask` doubles the charge of an answer with an invalid first output (tester quota only).
- "Добавь предмет по последней записи" only creates the subject; it does not link the recording to it (would be a second write).

## Follow-ups
- Tickets documents: without answer bounds 100 tickets can exceed `values.MaxDocumentRunes` (1M) in `homeworkdoc.BuildTickets`; per-ticket cut or a larger limit needed.
- ADR addenda (not edited here): ADR-0022 (no limits; Hub checks quote-in-fragment; per-part drop; find_lessons now carries names), ADR-0025/0026 (reask for ANSWER, reask after a stream; chunks are a draft).
- A dedicated error code for the invalid answer in `errors.proto` instead of matching the Hub message text.
- Ticket cost estimate (`ticketset/budget.go`) understates the worst case; comment updated, constants not changed.
- The two quote-matching copies (Hub `quote.go`, desktop `citation`) have no shared test vectors.
- The f01 retry offer is not scored automatically (read in the report).
- `docs/qa/tasks/TASK-364.md` quotes the old hint text «Нужна ссылка вида https://…»; superseded by this task.
- The Hub must be redeployed (answer v2, reask, panel v9).

## Changed existing tests
Hub `answer_test.go` (limit cases removed or turned into "nothing valid left" refusals, "at the limits" part dropped), `prompts_test.go` (`TestEmbeddedAnswerV1` -> V2; "answer" no longer without a reask note). Desktop: `answer_test.go`, `draft_test.go` (limits gone; fuzz check asserts decoded <= input), `homeworkdoc/tickets_test.go` (local figures instead of removed constants), `input/agent/tools_test.go` and `tests/agent_driver_test.go`, `agent_schedule_test.go` (find_lessons now carries names), `TestAnswerQuestionErrors` (per-row `refused`), `TestRunnerOnTheFakeHub`, call link tests (subject refusal row «no scheme» -> accept table, crosscheck corpus), frontend `callLink364ui.test.mjs` (new hint texts), fake Hub refusal sends the real Hub message.

## Review
Reviewer: changes requested (idempotency key, invalid-answer outcome, timeout, live cases, pin test, budget comment) -> fixed or reasoned; re-review: approve, with one medium (conflict mapping) and one low (f01 sent as tool error), both fixed after. Security-auditor: no findings (call-link scheme heuristic, validator, reask, find_lessons names, log leakage checked; removed limits are DoS/cost only on the tester's own quota).
