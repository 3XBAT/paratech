# TASK-230 — QA

No UI yet (form comes in TASK-231/232); check by tests.
1. From paratech/: `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/cases/homeworkflow/... ./desktop/internal/input/agent/... ./desktop/tests/...` passes.
2. Tests to look for: edit of title/note/subject/date+time (23:59 without time); unknown subject -> ErrSubjectNotFound; stale version -> conflict; deleted assignment not editable; agent edit_due keeps title and 09:15 and Undo restores it exactly (tests/agent_edit_due_test.go).
