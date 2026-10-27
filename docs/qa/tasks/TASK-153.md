# TASK-153 — owner's check

No UI for citations yet (TASK-155). Automated check from `paratech/`:
1. `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/cases/answering/... ./desktop/internal/cases/tickets/... ./desktop/internal/cases/knowledgebase/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/input/... ./desktop/internal/app/...` — passes.
2. `go test ./desktop/tests/...` — the architecture test passes.
Once notes are attachable (TASK-151+): in «Билеты» a ticket answered only from your note is «покрыт»; an uncovered one reads «Не найдено в лекциях и конспектах.»; the .docx cites «[источник: ваш конспект · стр. 3, …]». Ask a question about a subject with a note: the answer may cite «ваш конспект · …». After deleting the note, the old answer's source shows «источник удалён» (in the UI from TASK-155).
