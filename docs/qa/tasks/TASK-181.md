# TASK-181 — owner check

Hub-only contract; no visible change until TASK-183 (desktop pairs).

1. In paratech/: `go test -race ./hub/... ./shared/...` (Postgres as in CI) — green.
2. In paratech/shared: `buf lint`, `buf breaking --against master`, `buf generate` — no diff, no breaking.
3. `go build ./desktop/...` — builds.
4. End-to-end check ("what is a goroutine", then "who is Rybakov" and "Repeat" twice: answer only about the second question) is in docs/qa/tasks/TASK-183.md; the Hub from master with this task must be deployed first.
