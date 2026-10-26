# QA TASK-210

1. Tests: `go -C <repo>/paratech test -race ./desktop/internal/domain/agent/... ./desktop/tests/...` pass; `GOOS=windows go vet ./desktop/...` is clean.
2. Panel after the update: a question in the agent panel always gets a reply, and it names only screens that exist (the Hub now serves prompt v5 to this desktop).
3. A request left unfinished before the update: «Продолжить»/«Повторить» show the honest failure; a new request works.
