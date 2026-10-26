# TASK-192 — check steps

Core-only task; the UI part is TASK-193. Checks for now are automated:
1. `go test -race ./desktop/internal/domain/... ./desktop/internal/cases/turnkey/... ./desktop/internal/cases/homeworkflow/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/input/wails/...` passes.
2. The app starts on an existing database: migration 0034 applies, old sent-work cards still open.
3. After TASK-193 lands: rename the file in the library, then press «Да» on the pending send — the teacher's attachment keeps the name shown on the card; a question on a step with no connection / over quota shows its own text.
