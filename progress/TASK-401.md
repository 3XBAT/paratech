# TASK-401: E2E agent pack — withdraw, one-cut retry, step keys, no long holds

## Done (E2E only, no product code)
- 14-withdraw (new): a second question waits during a tool run, «Отменить этот запрос» returns its text into the empty field (focus), the first request ends with its answer, and the second text never reaches the fake (`llm.requests()`: only the 2 planner requests of the first question; no rule is scripted for it, so a leak fails as an unscripted call).
- 02-cut third test (new, `Feature.cutRetry`): one cut then a whole reply → «Повторная попытка 1 из 3», no «Ответ оборван», no banner, field enabled, 2 planner requests, one new `ag:` key without `:r`, 2 Hub attempts.
- 02-cut first test: after four cuts exactly one key K with 4 Hub attempts; «Повторить» adds exactly one new key `K:r1` (new key, K unchanged).
- Key evidence: neither the fake LLM request nor the Hub log carries the idempotency key, so the specs read `quota_idempotency_keys` of the run's throwaway Hub schema (`pgQuery` in `e2e/src/hub/pg.ts`, `agentStepKeys` in `specs/agent/support.ts`; read-only SELECT of `ag:` keys; the DB URL is never printed).
- Long `delayMs` holds dropped in 08, 10 (first test), 11, 13 (D1 fixed: cards stay after the reply); the rows act on the cards after the final reply. 13 re-checks the cards after 1.5 s. Holds left: the 8 s ANSWER hold in 12 and 14 (a mid-run state is the point), 01/03/05, and the D5 row (unchanged).
- D3, D4 stay green (TASK-399 rows); D5 (10-subject «Отменить» updates the tree) stays red as agreed with the owner (postponed).

## Real E2E (this machine, 09.11, heavy.sh, Docker up, `--no-build --project settings --project recording --project agent`)
38 rows: 37 готово, 1 сломано (D5). Agent project about 3.6 min (02-cut rows ~40 s each, retries 5+10+20 s).

Second full run after the review fixes (strict key rows): 36 passed, 2 failed: D5 and a FLAKE of 08-answer «карточка с источником» (passed in the first run on the same build). The failing line `08-answer.spec.ts:129` is unchanged by this task: the first fragment of the ANSWER request came from the recording «Человек в футляре · значок отмены» (made by 02a-retranscribe, same summary text), so the source page was not «основная»/«перезапуск». The order of equally ranked fragments is not fixed; the row's assumption is brittle (pre-existing, not relaxed here). Suggest a follow-up: make the source row's fragment order deterministic (a unique word only in the pack's recording) or accept any recording of the pack with that text.

## Decisions to confirm
1. `paratech/e2e/src/hub/pg.ts` (+ its unit tests) is outside the task's file list: the task asks for the key "по запросу к Hub или его логу", which carry no key; the Postgres read is the smallest honest evidence. No dependency added.
2. Also touched: `paratech/desktop/frontend/src/agent/README.md` (two doc lines naming row 14).
3. The withdraw row's 8 s hold must fit typing, seeing the queue label and the click; too short a hold fails loudly (unscripted call), never passes falsely.

## Follow-ups
- 13-cards-stay: if the action line is missing after the reply the subject stays in the library for later rows (reported, data polluted) — optional cleanup in a `finally`.
- pgQuery is read-only by convention; optional `SET default_transaction_read_only = on`.
- Full `npm --prefix paratech/e2e test`: the Docker-backed `hub.integration.test.js` hung >13 min on this machine in the tester's run (the other tests pass: hub-harness 29/29); three node processes (11672, 4080, 9512) were left running and need to be stopped by the owner/supervisor.

## Changed existing tests
No unit tests. E2E rows 02, 08, 10 (first test), 11, 13 changed only by removing the holds (waits and checks moved after the reply) and by the strict key checks; every behaviour assertion is kept. Reviewer (opus): 1 medium (key check vacuous when no key row) fixed and re-verified live; lows above. 

## Security
security-auditor (opus): none found (SQL identifier quoting, marker regexp, URL never in notes/errors).
