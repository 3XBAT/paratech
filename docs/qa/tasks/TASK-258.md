# TASK-258: check steps

Nothing is visible in the app yet (no caller until the call-link UI tasks).
1. `go test -race ./desktop/internal/adapters/weblink/...` from `paratech/` passes on Windows.
2. `GOOS=windows go vet ./desktop/internal/adapters/weblink/...` is clean.
