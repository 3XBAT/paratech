# TASK-215 — check steps

The task is core only (no UI, no agent tool yet); the owner's check is by tests.

1. From `paratech/`: `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/cases/homeworkflow/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/app/... ./desktop/tests/...` must pass.
2. `app/studentassignment_test.go` shows the visibility: an assignment created through the use case appears in the assignment list and «Горит на 7 дней» with the subject name, and Undo removes it.
3. The button (TASK-218/219) and the agent (TASK-217) come later; nothing changes in the running app yet.
