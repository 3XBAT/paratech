# TASK-215 — student-created assignments: core (ADR-0029)

## Done
- Domain `coursework/assignment/student.go`: `NewByStudent(StudentSpec, now)`, `StudentDue(date, time, loc)`, `ByStudent()`, `SameByStudent`, `MaxNoteRunes`, errors `ErrInvalidTitle/Note/Due/Time` (all match `ErrInvalidDetails`). Source -1, prompt 0, userEdited, subject as correction, teacher empty. No migration.
- Use case `homeworkflow` (create.go): `CreateAssignment`, `RemoveCreatedAssignment`, port `SubjectNames` (impl `subjectNames` in app/proposals.go over library.Service), errors `ErrSubjectNotFound`, `ErrNotByStudent`.
- sqlite: `DeleteUnused` + query `AssignmentInUse` (draft, delivery, work of any status, proposal not DECLINED/REJECTED, active draft-generation job), one transaction.

## Use-case API for TASK-217/218
```go
func (s *Service) CreateAssignment(ctx, in NewAssignment) (AssignmentView, error)
func (s *Service) RemoveCreatedAssignment(ctx, id shared.AssignmentID, expectedVersion int64) error
type NewAssignment struct { ID shared.AssignmentID; Title, Note string; Subject shared.SubjectID; Lesson shared.LessonID; Date, Time string }
type SubjectNames interface { SubjectName(ctx, shared.SubjectID) (string, error) } // Deps.Subjects
```
Create errors: field errors `ErrInvalidTitle/Note/Due/Time` (also `ErrInvalidDetails`); `ErrSubjectNotFound` (does not wrap ErrNotFound); unknown lesson wraps `shared.ErrNotFound`; same ID other content or extracted -> `shared.ErrDuplicate`; same ID same content -> stored view. View `Version` is 1 after creation. Remove errors: `ErrNotFound`, `ErrNotByStudent`, `shared.ErrVersionConflict`, `assignment.ErrInUse`.

## Decisions to confirm
- Note allows only \n and \r as controls (tab refused, stricter than NewDetails).
- No subject: correction stays empty; readers show the lesson's schedule subject or «Без предмета».
- "Draft in use" also covers an active draft-generation job (review finding); AssignmentInUse binds status lists as JSON arrays (sqlc slice mis-bound).

## Follow-ups
- A repeat of the same ID long after the first call may fail the due-window check instead of returning the stored row (documented in create.go); callers retry within seconds.
- `ByStudent()` = source == -1; legacy NULL-source rows would count (none expected).
- Turnkey writing jobs (`WorkWritingJobType`) not checked; the works table check covers works.
- Re-audit in TASK-217/218: idempotency ID derived by Core, never model output; `remove_assignment` only as compensation of the same action.
- Minor: add RETRY_WAIT test case; reword sqlc comment in assignments.sql.

## Changed existing tests
Fakes gained `DeleteUnused` (cases/homeworkflow/fakes_test.go, postprocess_fakes_test.go); no assertions changed.

## Security
Auditor: no high/medium. Reviewer: blocker (ports test PRD refs) and medium (queued draft job) fixed, re-review approved.
