# TASK-136 owner check

Nothing to check by hand: test-only change. Run `go test -race ./desktop/internal/adapters/sqlite/ ./desktop/tests/` from paratech/; expect about 35 s and 40 s.
