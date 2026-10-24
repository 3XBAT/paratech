# TASK-146: Desktop: streamed ANSWER and its draft text

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-142 (same files in `answering` and `answer`)
- **PRD sections / AC:** §9.2, §13.3; ADR-0022, ADR-0026 «Streaming»; hard rule 3
- **Branch:** ralph/<date>-task-146

## Goal
The ANSWER reader is read through `CompleteStream` and its partial output becomes plain draft claim texts for an
optional sink; the final answer is parsed and verified exactly as today.

## Allowed to change
- `paratech/desktop/internal/domain/coursework/answer/` new `draft.go` (+ tests, fuzz test)
- `paratech/desktop/internal/cases/answering/*` (+ tests)
- `paratech/desktop/internal/adapters/hubclient/{answer.go,answer_test.go}`
- `paratech/desktop/testenv/*` only for a fake `CompleteStream`

## Out of scope
- `input/agent`, `input/wails`, UI (TASK-147, TASK-148); planner steps.

## Acceptance criteria
- [ ] `answer.Draft(partial string) []string`: claim `text` values written so far (the last may be cut), JSON escapes
      decoded, control characters dropped, ≤ 8 texts of ≤ 400 runes; any input (fuzz) never panics.
- [ ] Port `answering.Reader.AnswerQuestion(ctx, req, partial func(soFar string))`; `partial` may be nil.
- [ ] `hubclient` uses `CompleteStream` with the same spec and key; hands the accumulated output to `partial` in order;
      `Unimplemented` → falls back to `Complete`, same key; error classes as today; a stream without `result` is an
      error and its text is discarded; the read limit applies to the total.
- [ ] `answering.Ask` takes an optional `DraftSink { Draft(texts []string) }`, calls it only with changed drafts, and
      never stores or returns draft text; parsing and verification run only on the final output.
- [ ] Tests: streaming fake server (deltas, result, error after deltas, unimplemented fallback, replay as one delta);
      draft fixtures; `go vet`, `go test -race` of the touched packages, architecture test pass.
