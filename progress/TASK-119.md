# TASK-119: Wails binding for the agent panel

Done: `input/wails` Agent binding (own narrow `AgentDriver` interface, no input/agent import), `AgentPanel` sink emitting `agent:update` (DTOs: ids, enums, validated plain text), `State` (link/readOnly), Ask gated offline, app wiring (`panelSink`, Driver ctx = app `startCtx`, Close on shutdown). `Undo` and `History` are stubs ("unavailable").

## Decisions to confirm
- architecture.md sec. 6 says only domain types cross the Agent interface; the binding passes cases types (`AskInput`, `TurnRef`, `connections.View`) like the other input/wails bindings (20 files import cases). Docs not edited (needs an ADR); follow-up: align the sentence.
- Read-only/offline reflects Hub configuration only; an outage shows as `stopReason: "offline"` on an update.
- Review: approve (medium: ordering comment, architecture wording; lows fixed: deferred detach, escape assertion). Security auditor: no High/Medium.

## Follow-ups
- History read (90 days): no read use case in cases/agentturn (TASK-123 area); then map via `AgentUpdateView`.
- Per-thread monotonic version in Update so the panel can drop stale updates across operations.
- TASK-120: do not rely on arrival order across operations; failed Asks arrive with empty threadId (match by scope); render model text as text only (no innerHTML); artifact ref is "<kind>/<path>"; read-only has two sources (config, stopReason offline).
- Ask carries no upload ids.

## Changed existing tests
`input/wails/shell_test.go` (validCore has Agent), `settings_test.go` (TestOpenBindings checks the Agent parts).
