# QA TASK-147: answer draft event (core)

No UI rendering yet (TASK-148), so the owner checks through tests.

1. `go -C <repo>/paratech test -race ./desktop/internal/input/... ./desktop/internal/cases/answering/... ./desktop/internal/app/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/...` (architecture test) passes; `GOOS=windows go vet ./desktop/...` is clean.
3. Read `input/wails/agentdraft_test.go`: a draft every 1 ms for 1 s gives 11 events; the flush comes before the card update; Stop, a failed request and a dropped end cut later drafts.
4. Read `input/agent/draft_test.go`: drafts come before the card, bidi/zero-width characters are stripped, a failed read leaves no card, the draft text is absent from the result and the log.
5. `npm --prefix <repo>/paratech/desktop/frontend run test` passes (`parseDraft`).
