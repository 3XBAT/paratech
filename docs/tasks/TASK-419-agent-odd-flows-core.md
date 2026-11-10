# TASK-419: agent — odd flows on the fake Hub (level 1): Stop mid-plan, «Повторить» after a failure, Hub down between steps, quota mid-plan, odd model output

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** —
- **PRD sections / AC:** §6, AC-11, AC-18; ADR-0021, ADR-0026 (+ addendum 2026-11-08), ADR-0033 §4
- **Branch:** ralph/<date>-task-419

## Goal
Deterministic scenario tests (CI every run) on the real Driver, use cases and SQLite with only the Hub faked, for the
flows the owner wants thrown at the agent. Each scenario checks: what the Driver executed (never twice), the cards
and stop reason the panel gets, the step keys sent, and that the request ends (no hang).

## Allowed to change
- `paratech/desktop/tests/agent_odd_flow_test.go` (new)
- `paratech/desktop/testenv/agenthub.go` + `agenthub_test.go`: new knobs only
  - `ThenQuotaExceeded()` — the next step answers `resource_exhausted` with ErrorDetail `QUOTA_EXCEEDED`;
  - `GoDown()` / `ComeBack()` — while down, the client of `StartFakeAgentHub` fails every new request before it reaches
    the server with a dial error (`*net.OpError{Op: "dial"}` wrapping `syscall.ECONNREFUSED`), as a Hub that is not
    running (`hubclient` `connRefused` matches it on Windows and Linux).
- `paratech/desktop/internal/cases/agentturn/*`, `paratech/desktop/internal/input/agent/*`: only a fix of ≤ ~50 lines for
  a defect a scenario shows; anything larger: progress note and stop.

## Scenarios (check `agent_eval_test.go`, `agent_driver_test.go`, `agent_sqlite_test.go`, `agent_stream_test.go`
first and do not duplicate what they already pin)
1. **Stop mid-plan:** `show_plan` with 3 `create_folder` steps → «Да» → after step 1 ran and while the next Hub step
   is held (`ThenStreamHeld`), Stop → stopped `user`; exactly 1 folder exists; Continue finishes steps 2–3, no step
   twice, journal has 3 actions.
2. **«Повторить» after a failure:** non-retryable provider error → stopped `failed`, card retry/cancel; Retry → key
   `…:r1`, scripted reply; one reply card.
3. **Hub down between steps:** step 1 `find_lessons` runs; `GoDown()`; the results step fails → automatic retries
   (on the managed clock) → stopped `offline`; `find_lessons` ran once; `ComeBack()`, Retry → done. A question asked
   meanwhile waits, starts after the stop and also stops `offline` with its own card (TASK-398 decision 2), no hang.
4. **Quota mid-plan:** plan approved, step 2 `ThenQuotaExceeded()` → stopped `quota`, no automatic retry; Continue
   later repeats the same key and finishes.
5. **Second question while a tool runs:** at Driver level, held tool; the second waits (ticket), starts by itself
   after the first, the Hub sees its text once and after the first answer.
6. **Odd model output:** unknown tool name; arguments that are not JSON; `reply` with empty text; `ask_questions`
   with no options; a C tool with an ID the model never saw; two calls with the same call ID — each refused with the
   existing refusal (`is_error`), nothing executed, the request still ends.

## Acceptance criteria
- [ ] All scenarios green; each asserts executed tools, cards, stop reason, keys and the end of the request.
- [ ] Knobs have their own tests in `testenv/agenthub_test.go`.
- [ ] `go test -race ./desktop/tests/... ./desktop/testenv/...` (+ touched packages) and the architecture test;
      `GOOS=windows go vet ./desktop/...`; `golangci-lint run ./desktop/...`.
