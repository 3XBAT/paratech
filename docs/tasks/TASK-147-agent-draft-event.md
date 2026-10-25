# TASK-147: Draft text of an answer to the panel (Driver and Wails event)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-141, TASK-146
- **PRD sections / AC:** ADR-0021, ADR-0026 «Streaming»; hard rule 3
- **Branch:** ralph/<date>-task-147

## Goal
While `answer_question` runs, its draft texts reach the panel as a throttled Wails event bound to the request and call.

## Allowed to change
- `paratech/desktop/internal/input/agent/{driver.go,answer.go,tools.go}` (+ tests, fakes)
- `paratech/desktop/internal/input/wails/agentpanel.go` and a new `agentdraft.go` (+ tests)
- `paratech/desktop/internal/app/*` (wiring)
- `paratech/desktop/frontend/src/bindings/agent.ts`: `draftEvent = "agent:draft"` and its parser only

## Out of scope
- Rendering (TASK-148); the Hub.

## Acceptance criteria
- [ ] `Sink` gets `Draft(d Draft)` with `Draft{Thread, Turn, CallID, Texts}`; the Driver passes a `DraftSink` bound to
      (thread, turn, call) to `Answerer.Ask`; nothing else uses draft text (not in results, history, journal or log).
- [ ] `input/wails` emits `agent:draft {threadId, turn, callId, texts}` at most 10 times a second per call, last value
      wins, a final flush before the step's update; texts are strings only.
- [ ] After «Стоп» or a cut read no further draft of that call is emitted.
- [ ] Tests: throttling with a fake clock, order (draft before the card update), stop cuts drafts, payload shape.
- [ ] `go vet`, `go test -race ./desktop/internal/input/...`, architecture test, `GOOS=windows go vet ./desktop/...`.
