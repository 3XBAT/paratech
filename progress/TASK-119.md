# TASK-119: Wails binding for the agent panel (PAUSED, not pushed)

Branch ralph/20261022-task-119. Commits c3a87f51 (binding + events in input/wails), b2a84b6b (app wiring). Paused by the supervisor before push/PR.

## Done
- input/wails Agent binding (own narrow AgentDriver interface, no input/agent import), AgentPanel event sink (`agent:update`), app wiring (panelSink, app-lifetime ctx = startCtx, Close on shutdown). Undo and History are stubs ("unavailable").
- Verified (implementer): go test -race of input/wails, app, input/agent; arch tests; vet (Windows and linux); golangci-lint 0 issues.
- Reviewer: approve (no blocker/high). Security auditor: no High/Medium.

## Left (next steps to resume)
1. Fix review items: soften the ordering comment in agentpanel.go (order holds only within one Driver goroutine); `defer b.AgentPanel.Attach(nil)` after Attach(app.Event) in shell_windows.go; fix weak `"<"` assertion in agent_test.go (~L478); decide architecture.md sec. 6 wording ("domain/cases types") - needs its own explicit step, report as flag.
2. Commit these, re-run touched scope, fetch, rebase if master moved, push, `gh pr create --body-file`, run `.github/scripts/audit-pr.sh <pr>`, report. Do not merge.
3. Write docs/qa/tasks/TASK-119.md; set tasks.json status for TASK-119.

## Follow-ups
- History read (90 days): no read use case in cases/agentturn (TASK-123 area); needs one, then map via AgentUpdateView.
- Hub unreachable is not tracked; read-only reflects configuration only; outage shows as stopReason "offline".
- TASK-120 notes: do not rely on arrival order across operations (add per-thread version later); failed Asks arrive with empty threadId, match by scope; render model text as text only (no innerHTML); artifact ref is "<kind>/<path>".
- No uploads in Ask.

## Changed existing tests
input/wails/shell_test.go (validCore has Agent), settings_test.go (TestOpenBindings checks Agent parts).
