# TASK-221 — check steps

Hub-only change.

1. `go test -race ./hub/... ./shared/...` in `paratech/`: all green; `TestPlanStepNoteLimit` is in toolcatalog.
2. After the Hub restart (live, level 2): ask for several assignments at once with a long note (over 80 characters) for one of them; the plan card appears and the step is not refused (no PROVIDER_ERROR).
