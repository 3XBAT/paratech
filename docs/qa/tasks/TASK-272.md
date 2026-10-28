# TASK-272 — owner's check

No visible UI change yet: the panel uses `chatCurrent` in TASK-273. Nothing to click now.
Check: CI is green; `go test ./desktop/internal/cases/agentturn/... ./desktop/internal/input/...` pass.
After TASK-273: ask the agent for a plan, close the app while it waits for the plan's «Да», reopen, open the chat: the card with «Да»/«Стоп» is shown.
