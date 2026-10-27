# TASK-151 — QA

No UI yet (TASK-154); check by tests, from paratech/:
1. `go test -race ./desktop/internal/cases/notes/... ./desktop/internal/cases/library/... ./desktop/internal/input/wails/... ./desktop/tests/...` passes.
2. Look for: same bytes twice -> «уже добавлено»; file of another subject refused; removed notes absent from lists; recording purged -> note reads as its day; subject with notes -> `ErrHasNotes`, nothing deleted; no path in any binding payload.
3. `GOOS=windows go vet ./desktop/...` is clean.
