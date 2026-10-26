# TASK-188 — owner's check

The card is not drawn until TASK-189, so the check is by tests until then.

1. `go test -race ./desktop/internal/domain/agent/... ./desktop/internal/input/agent/... ./desktop/internal/input/wails/... ./desktop/internal/adapters/sqlite/...` (from `paratech/`) — passes.
2. After TASK-189: ask the agent «сделай презентацию на тему X» with no assignment. A card «Такого задания нет — могу сделать как свободную работу» appears; «Сделать» starts the triage (one confirmation, no plan card); a second click does nothing new; Undo removes the work.
3. «Не надо»: nothing is spent. Tick «Больше не показывать»: the next topic request starts the triage at once; Settings has the switch «Предупреждать перед свободной работой» (on by default).
