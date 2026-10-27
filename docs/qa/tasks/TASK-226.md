# TASK-226 — owner's check

Backend only (UI is TASK-227).
1. `go test -race ./desktop/internal/domain/coursework/work/... ./desktop/internal/cases/turnkey/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/input/wails/...` from `paratech/` passes.
2. With the app: start a triage offline (network off) on an assignment, restart the app: the work DTO (`TurnkeyWork`) has `lastProblem":"offline"` and `lastMessage` set; a successful retry clears it.
3. Send a work email via a confirmed send_work_email proposal; after it is SENT, open the work: `sentAt` is the send time (RFC 3339 UTC), also after restart.
