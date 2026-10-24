# TASK-142: Answer reader key scoped to the agent request

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** - (owner question 1 of ADR-0026; if the owner wants the stored answer on a repeat, drop this task)
- **PRD sections / AC:** ADR-0022, ADR-0026 «Повторить» (idempotency); hard rule 7
- **Branch:** ralph/<date>-task-142

## Goal
A repeated request gets a fresh `ANSWER`, while «Продолжить» in the same request still replays the paid one.

## Allowed to change
- `paratech/desktop/internal/domain/coursework/answer/request.go` (+ tests)
- `paratech/desktop/internal/cases/answering/*` (+ tests)
- `paratech/desktop/internal/input/agent/{answer.go,tools.go}` (+ tests)

## Out of scope
- Streaming (TASK-146), Driver loop (TASK-140/141).

## Acceptance criteria
- [ ] `answer.NewRequest` takes a reuse scope (opaque, bounded printable ASCII); the key is `ans:v2:` + SHA-256 over
      data, places and scope; an empty scope is refused.
- [ ] `answering.Ask` takes the scope; the Driver passes `ag:<thread>:<turn>`.
- [ ] Same question, same fragments, same request → same key; another turn → another key (tests).
- [ ] Answers stored under `ans:v1:` still load (stored state validation accepts both prefixes).
- [ ] `go vet`, `go test -race` of the touched packages and the architecture test pass.
