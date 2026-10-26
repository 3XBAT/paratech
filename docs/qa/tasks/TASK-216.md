# TASK-216 — check steps

Hub-only change; the desktop still uses catalog 4 until TASK-217, so nothing is visible in the app yet.

1. `go test -race ./hub/... ./shared/...` in `paratech/` (Postgres as in CI): all green.
2. `shared/testdata/tool_catalog_v6.json` equals `hub/pkg/prompts/agent/panel/v6/tools.json`.
3. After TASK-217 (live, level 2): say "завтра в 9 скинуть проверочную Иванову" — an assignment with tomorrow 09:00, no email proposal; say "к пятнице" on a Friday — the assistant asks which Friday.
