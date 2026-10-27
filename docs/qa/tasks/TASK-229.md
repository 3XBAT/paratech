# TASK-229 — QA

No UI yet (buttons come in TASK-231/232); check by tests.
1. `go test -race ./desktop/internal/cases/homeworkflow/... ./desktop/internal/app/... ./desktop/tests/...` from paratech/: passes.
2. Tests to look for: delete -> removal row and event removed (fake Google); restore before sync -> no row, event stays; «Да» on each proposal kind after delete -> STALE assignment_deleted; recording deletion keeps a deleted assignment with transcript NULL.
