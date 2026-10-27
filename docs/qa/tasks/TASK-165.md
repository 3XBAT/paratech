# TASK-165 — QA

No UI for pages yet (TASK-154); check by tests, from paratech/:
1. `go test -race ./desktop/internal/adapters/imagepage/... ./desktop/internal/domain/library/... ./desktop/internal/cases/library/... ./desktop/internal/cases/notes/... ./desktop/internal/input/wails/... ./desktop/internal/adapters/sqlite/... ./desktop/tests/...` passes.
2. Look for: a JPEG with GPS comes out without it; 60000x60000 header, PNG bomb and truncated file refused; HEIC/SVG refused; same photo twice -> «уже добавлено»; a photo is never read, searched or cited («текст не распознан»); `/media/notepage/<id>` gives image/jpeg or image/png only.
3. Optional fuzz: `go test ./desktop/internal/adapters/imagepage -run xxx -fuzz FuzzClean -fuzztime 30s`.
4. `GOOS=windows go vet ./desktop/...` is clean.
