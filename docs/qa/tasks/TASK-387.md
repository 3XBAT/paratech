# QA TASK-387: Local Hub, domain and use case

Nothing visible to the student or the owner yet: no UI, adapter or wiring (TASK-388..391). Cold app: unchanged, no Hub is started by this task.

Check steps (developer):
1. `go -C <repo>/paratech test -race ./desktop/internal/domain/localhub/... ./desktop/internal/cases/localhub/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/ -run TestRealTree` passes (rule «beta-only packages»).
3. Read `progress/TASK-387.md` «Decisions to confirm» (exit handling, start-failure display) before TASK-389 and TASK-391 start.
