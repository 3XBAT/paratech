# TASK-152 — owner's check

No UI yet (TASK-155): the current «База знаний» page looks as before. Automated check from `paratech/`:
1. `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/cases/knowledgebase/... ./desktop/internal/input/wails/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/app/...` — passes.
2. `go test ./desktop/tests/...` — the architecture test passes.
Once notes are attachable (TASK-151+): a subject export (Markdown) gets a «Мой конспект» block per lecture with the line «ваш конспект, текст без изменений»; a note-only lecture appears by date, undated at the end. A note on a recording that is still processing shows as a separate day lecture until it is ready.
