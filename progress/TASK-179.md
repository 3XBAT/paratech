# TASK-179: AskStep backend («Спросить по шагу»)

## Done
- `prompts/advice.go`: versioned prompt (`StepAdviceVersion = 1`) on `homework_draft`; requirements, step and question go as an HTML-escaped JSON data block; key = `work.StepQuestion.Key()` + `:p1`.
- `hubclient/advice.go`: `Client.StepAdvisor(alias)` implements `turnkey.StepAdvisor` over `Complete` (no Hub/proto change); permanent provider refusal also matches `work.ErrInvalidAnswer`.
- `app/core.go` wires the Advisor with the writing alias. The use case (`CheckAdvice`, `readerError`) and the `TurnkeyService.AskStep` contract are unchanged.
- ADR-0025 addendum; stale "no operation / unavailable" wording removed.

## Decisions to confirm
- Key comes from the existing domain `StepQuestion.Key` (work id + data incl. step text), not a literal step number; identical steps share a key, harmless as the request is identical.
- Prompt version is in the key so a prompt change makes a new call instead of an idempotency conflict.

## Follow-ups
- (review medium) No output-token cap on the advice call: `LLMRequest` has no max-output, so the writing alias cap applies; add a ~1000-token cap (touches `coursework/values` and `specOf`).
- (review medium) A stored over-long/invalid answer replays as refused forever for the same question; student must reword. Consider a "rephrase" hint in the binding's refused message.
- (review low) Model alias is not in the key; changing the alias makes old questions hit idempotency conflict.
- (review low) Errors carry op "hubclient: complete" instead of the advice op.
- (security low) "No links or emails" is only a prompt instruction; could be enforced in code.
- Binding shows the generic error for quota/refused/unavailable on AskStep; frontend (`turnkeyView.ts`, `bindings/turnkey.ts`) still describes "no backend yet"; `desktop/tests/turnkey_test.go` has no end-to-end AskStep test.

## Changed existing tests
None (`app/turnkey_test.go` got a new case: AskStep of an unknown work now answers not-found, proving the advisor is wired).

## Security
Auditor: no high/medium. Reviewer: approve, 2 medium (follow-ups above), 3 low.
