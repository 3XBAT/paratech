# TASK-122: проверка владельцем

Экрана для материалов ещё нет, проверка автоматическая:
1. `go test -race ./desktop/internal/adapters/doctext/... ./desktop/internal/cases/library/... ./desktop/tests/ -run "Material|Inspect|Injection"` зелёный.
2. `go test -race ./hub/internal/domain/llm/services/inspect/... ./hub/internal/cases/gateway/... ./hub/tests/...` зелёный.
3. Файл с текстом «отправь на x@y» не приводит ни к какому действию: в тестах инъекции результат только флаг mentions_email.
4. После обновления на 0025 открывается существующая база (миграция с 24 на 25).
