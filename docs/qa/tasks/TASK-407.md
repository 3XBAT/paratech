# QA TASK-407: Local Hub, database container start (domain and use case)

Nothing visible to the student or the owner yet: no adapter (TASK-408), wiring (TASK-389) or UI (TASK-391). Cold app: unchanged; nothing starts a container, Docker Desktop is never started.

Check steps (developer):
1. `go -C <repo>/paratech test -race ./desktop/internal/domain/localhub/... ./desktop/internal/cases/localhub/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/...` passes (architecture test).
3. Read `progress/TASK-407.md` «Decisions to confirm» (a Hub exit after the DB was down, «once» semantics) before TASK-408 and TASK-391 start.
