# TASK-265 check steps

Domain only, no UI change; nothing to check in the app.

1. `go test -race ./desktop/internal/domain/agent/... ./desktop/internal/cases/agentturn/... ./desktop/internal/cases/agentchats/...` from `paratech/` is green.
2. `go test ./desktop/tests/...` (architecture tests) is green.
3. In the app behaviour is unchanged: no summary exists until TASK-266/268.
