# TASK-218 — Wails CreateAssignment + bindings/homework.ts

## Done
- `input/wails/homeworkcreate.go`: `HomeworkService.CreateAssignment(CreateRequest) CreateResult{problem, field, assignment}`; no error return, every outcome is a code. `HomeworkProposals` port gained `CreateAssignment`; `app` unchanged (same `homeworkflow.Service`).
- requestId: 1..64 `[A-Za-z0-9-]`, else `invalid` with field `requestId` (not called into the use case). Assignment ID = `ui-` + hex(sha256("ui:"+requestId)[:16]).
- Mapping (errors.Is): ErrInvalidTitle/Note/Due/Time -> invalid + field; other ErrInvalidDetails -> invalid, no field; ErrSubjectNotFound -> subject_not_found; shared.ErrNotFound -> lesson_not_found; ErrDuplicate and others -> failed (cause logged only).
- No agent journal entry (the student's own click). Frontend `bindings/homework.ts`: `createAssignment` with strict parsing.

## Decisions to confirm
- `field: "requestId"` is an addition to the task's list (title|note|due|time); TASK-219 should show the general text for it.

## Follow-ups
- TASK-219: regenerate requestId after a `failed` reply if the form was edited (same token with different content gives ErrDuplicate -> failed).
- Optional: log context.Canceled below Error level (consistent with other methods now).

## Changed existing tests
- `settings_test.go` TestFrontendCallsBoundNames: HomeworkService method count 17 -> 18. `homework_test.go`: fake and everyHomeworkCall extended, no assertions changed.

## Security
Reviewer: approve, 3 lows. Auditor: no findings (token validated and hashed, no overwrite possible, no error text to page).
