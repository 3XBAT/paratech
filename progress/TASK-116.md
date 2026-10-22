# TASK-116: agent core (desktop cases, hubclient, migration) + Hub thinking retention

Done: `cases/agentturn` (Ask, Submit, ApprovePlan, RevisePlan, AnswerQuestions, Stop, Continue, Retry, Restart, Cancel, Recover), `cases/agentjournal` (Begin/Finish, Undo, List, Recover), `hubclient.Step`, desktop migration 0021 + sqlite repositories (CAS), app wiring, SQLite+fake-Hub scenario. Hub: raw assistant turn (thinking + signature + tool_use) kept in Postgres `agent_turns` (migration 0007, 24 h, scoped by API key), re-inserted on the last echo; missing turn = `failed_precondition` (ErrTurnGone) before any provider call; fake Messages API rejects a continuation without the thinking block. Supervisor items 1-5 all done (created-ID binding, paid reads not counted, 3rd question round -> assumptions, thinking retention, new key on user retry, max 3 retries).

## Decisions to confirm
- Hub keeps the turn in Postgres (not memory); ADR-0023 updated. Turn not part of the reservation (replay stays free).
- H1 fix: earlier requests go to the Hub as student texts only; only the current request carries echoes. The model no longer sees its earlier replies. ADR-0021 says "action lines": not implemented yet. Quality effect to be judged by the owner.
- Retry = key suffix `:r<N>`, max 3, then budget stop. Continue = same key (network/quota unknown outcome). Turn gone: silent restart only if nothing done yet.
- Deferred plan IDs are only for needed IDs; a call must carry them.
- Migration number 0021 (renumber if TASK-121 merges first).

## Follow-ups
- `UnknownFacts` placeholder (ReplacesReady=false) must be replaced by the Driver task (TASK-117) before tools run.
- Compensator contract after a crash between Apply and UNDONE gives UNDO_REFUSED for in-place compensations: Apply/TargetVersion must recognise an already compensated target (reviewer low).
- Stop retries a version conflict once only; give it a bounded retry (reviewer low). Plan approval / question answers still lose the click on a racing Stop.
- `historyFrom` could be dropped since earlier echoes are never sent. ADR-0021 text on shrinking to action lines.
- Subject list in base empty until TASK-121; no 90-day purge of agent_steps; no desktop cap on a single request above 256 KiB; paid reads uncapped by count (steps x calls bound them); global `mu` in agentturn holds across Hub call.
- Hub: gateway.proto comments do not mention `failed_precondition` for AgentStep; verify live that earlier-turn thinking may be omitted and Go re-encoding of raw content is accepted.
- Hub Postgres tests ran against a Postgres on port 15432 (another coordinator's); not CI.

## Changed existing tests
`sqlite/migrate_test.go` (4 tables, version 21), `tests/schema_test.go`, `random_test.go` rows; Hub `agentstep/service_test.go` fixture (Turns dep), `tests/agent_test.go`, `connect/agent_test.go`; `TestAskClosesWaitingRequest` (now asserts the result in stored history and texts-only to the planner, due to H1).
