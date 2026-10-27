# QA TASK-225 — subject page data

No UI yet (TASK-227); the check is by tests.
1. `go test -race ./desktop/internal/cases/subjectview/... ./desktop/internal/adapters/visitfile/... ./desktop/internal/domain/library/... ./desktop/internal/input/wails/... ./desktop/internal/app/...` from `paratech/`: all pass.
2. `GOOS=windows go vet` on the same packages: clean.
3. After a real run with TASK-227: the file `subject-visits.json` appears in the data folder only after a subject is opened; deleting it just removes the «новое» marks.
