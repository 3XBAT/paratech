# QA TASK-178: «Остановить» writing, running-job flags, EditOutlineAt (core)

No UI change yet, so the owner checks through tests.

1. `go -C <repo>/paratech test -race ./desktop/internal/cases/turnkey/... ./desktop/internal/input/wails/... ./desktop/internal/app/... ./desktop/internal/domain/coursework/work/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/...` (architecture test) passes.
3. `npm --prefix <repo>/paratech/desktop/frontend run test` and `run build` pass.
4. Read `cases/turnkey/stop_test.go`: stop of a queued/running/waiting job, of a finished job and with no job; active/jobState per job state; stale plan version and no-op edit keep the version.
