# TASK-194 — owner's check

The change is data only (no visible UI until TASK-195).

1. From `paratech/`: `go test -race ./desktop/internal/input/wails/...` passes.
2. `GOOS=windows go vet ./desktop/...` is clean.
3. After TASK-195 lands: a presentation work card shows slides, questions and the talk; a lab report card shows sections and the places to fill in.
