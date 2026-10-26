# TASK-192 — follow-ups of 26.10 (core)

## Done
- Work DTO (`TurnkeyWork`, input/wails/turnkey.go) now has `topic` (string), `subjectId` (string), `sendable` (bool, from `Work.Sendable()`). Names are stable for TASK-193; the TS bindings are not mapped yet.
- `ErrDraft*`, `ErrGenerateRetries`, `ErrGenerationRunning` and two generation messages say «черновик»; the same strings in `bindings/homework.ts` `errorMessages` (a Go test compares them), keys unchanged.
- AskStep: own texts `ErrTurnkeyStepOffline`, `ErrTurnkeyStepQuota`, `ErrTurnkeyStepRefused` (TASK-193 adds them to `bindings/turnkey.ts`); `ErrTurnkeyNoStepHelp` now means only "no advisor wired".
  Own output cap: `LLMRequest.WithMaxOutputTokens`, `prompts.StepAdviceMaxOutputTokens = 1024`, `StepAdviceVersion` 1 -> 2 (key suffix `:p2`). `LLMResult.Replayed`; `StepAdvisor` returns `(out, replayed, err)`; `StepQuestion.Attempt(n)`, `MaxAdviceAttempts = 3`: a replayed bad answer is free and moves to the next key, a fresh bad one stops the ask (at most one paid call per ask, three per question).
- Work send: copy name pinned in the proposal (`Facts.AttachmentName`, pinned by `Validate` inside `ProposeSendWork` before the first save). Migration `0034_work_send_name.sql` (`proposals.attachment_name`), schema.sql, migrate_test version 34. Restore accepts a stored name only on a post-validation send of a work and only if it is a fixed point of `CopyName`. Sends stored before fall back to the library name.
- `SafeFileName` drops bidi (U+202A-202E, U+2066-2069, U+200E/F, U+061C) and zero-width (U+200B-200D, U+2060, U+FEFF).

## Decisions to confirm
- Re-proposing after a rename reuses the waiting send with its old pinned name (so two «Да» never mean two emails).
- Frontend test `homework.test.mjs`: `bindings/homework.ts` added to the files allowed to contain «черновик», and `homework.go` removed from the Go-literal check of that guard (wording change).
- sqlc is not installed here: `sqlcgen/proposals.sql.go` and `models.go` were edited by hand; if the CI drift check fails, regenerate.
- Migration number 0034 taken at push time.

## Follow-ups
- TASK-193: map the new DTO fields and AskStep texts in the TS bindings.
- Other «решение» texts left (out of ErrDraft* scope): `msgGenerationFailed`, `rejectMessages[ReasonNoArtifact]`, the TASK-102 «черновик» guard exemption.
- ADR-0025 addendum (migration 0034, advice cap, prompt v2, attempt keys) not written.
- `SafeFileName` keeps rare invisible Cf characters (U+00AD, U+2061-2064, U+206A-206F); reviewer suggests dropping all `unicode.Cf` (low).
- Advice errors still carry op "hubclient: complete" (TASK-179 low).

## Changed existing tests
migrate_test (v34); proposalworksend_test migration-33 test migrates to 34; homeworkflow worksend_test (failed-read check moved to ConfirmSendWork); proposal worksend_test (pinned name); turnkey fakes_test (advisor replay); hubclient/prompts advice tests (`:p2`, cap); wails turnkey_test golden JSON; homework_test/homeworkdocx_test and frontend homework.test.mjs (new wording).

## Review / security
Reviewer: one blocker (frontend test not updated) fixed; rest approved. Security auditor: no findings.
