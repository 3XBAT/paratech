# QA TASK-174: «Отменить» on a finished job (core)

No UI change (TASK-175), so the owner checks through tests.

1. `go -C <repo>/paratech test -race ./desktop/internal/domain/agent/action/... ./desktop/internal/cases/agentjournal/... ./desktop/internal/input/wails/... ./desktop/internal/input/agent/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/...` (architecture test) passes.
3. Read `cases/agentjournal/jobundo_test.go`: a finished job gives undoable=false, noUndo=result_ready; a running one is undoable; Undo of a ready result leaves the entry DONE; the race gives UNDO_REFUSED.
