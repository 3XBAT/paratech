# TASK-153: Answers cite the student's notes; notes count in ticket coverage

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-152
- **PRD sections / AC:** ADR-0027 «Knowledge base, answers, tickets»; ADR-0022, ADR-0025 (tickets); TASK-118; hard rule 3
- **Branch:** ralph/<date>-task-153

## Goal
An answer can cite «ваш конспект · стр./раздел N», and a ticket covered only by the student's notes is covered.

## Allowed to change
- `paratech/desktop/internal/domain/coursework/answer/**`
- `paratech/desktop/internal/domain/coursework/services/knowledgebase/passages.go` (+ tests)
- `paratech/desktop/internal/cases/answering/**`, `paratech/desktop/internal/cases/tickets/**` (tests, wording)
- `paratech/desktop/internal/adapters/sqlite/{answers.go,answers_test.go}`
- `paratech/desktop/internal/input/wails/{answers.go,tickets.go}` (+ tests), `paratech/desktop/frontend/src/bindings/{answers.ts,tickets.ts}` if present
- `paratech/desktop/internal/input/agent/answer.go` (+ tests) if the source mapping lives there

## Out of scope
- Hub prompts (unchanged: fragments are data), UI rendering (TASK-155).

## Acceptance criteria
- [ ] `answer.Source` gets section `ownNote` with note ID, material ID and locator (kind, N); the recording is empty
      for it and required for the others; stored answers of TASK-118 still load.
- [ ] `Passages` takes note fragments; on an equal score the student's fragment ranks before the AI summary.
- [ ] Citation check unchanged (`coursework/services/citation`); a quote found only in a note is a valid citation.
- [ ] Tickets: a ticket answered from a note is `covered`; the not-covered text reads «нет в ваших лекциях и
      конспектах».
- [ ] Injection fixture: a note fragment with a command stays data; the planner gets counters only.
- [ ] `go vet`, `go test -race` of the touched packages and the architecture test pass.
