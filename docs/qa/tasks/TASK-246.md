# TASK-246 — QA

The binding has no UI yet (UI is TASK-247). Check by tests only:

1. `go test -race ./desktop/internal/input/wails/... ./desktop/internal/app/...` from `paratech/` passes.
2. `npm --prefix paratech/desktop/frontend run test` passes (agentmentions.test.mjs).
3. After TASK-247: type «@» in the agent field, see subjects, lectures, materials and assignments; a deleted assignment never appears.
