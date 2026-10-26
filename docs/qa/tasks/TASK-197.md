# TASK-197 — check steps

The printer has no UI yet (wired in TASK-198). Check from `paratech/`:

1. `go test -race ./desktop/internal/adapters/pdf/...` on Windows with WebView2 Runtime installed: all pass (about 20 s); it skips the real-print tests if the runtime is missing. Your own WebView2 data folder is not touched (tests use temp folders).
2. `GOOS=windows go vet ./desktop/...` and `GOOS=linux CGO_ENABLED=0 go vet ./desktop/...` are clean.
3. `go test ./desktop/tests/...` (architecture test) passes.
4. Afterwards no stray `msedgewebview2.exe` processes started by the tests remain.
