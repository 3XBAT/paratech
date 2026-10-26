# TASK-194 — presentation and lab report data in the work card DTOs

## Done
- `TurnkeyWriting` gets presentation, template, questions, talkMaterialId, talkResult, lab, manual, placeholders;
  `TurnkeySection` gets kind, part, bullets, columns. Filled in `writingView`; empty lists are `[]`.
- Only input/wails/turnkey.go and turnkey_test.go changed. No new methods, no use case/domain changes, `TurnkeyOutlineItem` untouched.
- No TS change: the hand-written parsers ignore unknown keys; TASK-195 adds the fields to WorkSection/WorkWriting and the parsers.
- Tests: new `TestTurnkeyWritingKinds` (presentation, lab, ordinary work).
- Changed existing test: `TestTurnkeyWritingJSON` expected JSON gets the new keys; old values unchanged.
- Verified: go test -race of input/wails, GOOS=windows go vet ./desktop/..., lint of the wails package, architecture test.

## Decisions to confirm
- The new fields are always present in the JSON (no omitempty): false / "" / [] for an ordinary work.

## Follow-ups
- Reviewer nit: the `TurnkeyWriting` comment should say a lab report's `manual` is "" until the outline is set (domain/coursework/work/lab.go:69); TASK-195 must accept "".
- Reviewer nit: no test for an unfinished presentation (talkMaterialId/talkResult must be ""); the use case decides it.
- TASK-195 must render all these model-written texts as plain text only (no dangerouslySetInnerHTML, no links/paths from them).

## Review
- Reviewer: approved, no blockers (3 small points above/bookkeeping).
- Security: no findings.
