# TASK-182: check steps

Hub-only change; the desktop still uses catalog 3 until TASK-184, so nothing changes in the UI yet.

1. In `paratech/`: `go test -race ./hub/pkg/prompts/... ./hub/internal/domain/llm/... ./shared/... ./hub/tests/...` passes.
2. Open `hub/pkg/prompts/agent/panel/v4/tools.json`: no `generate_draft`; `triage_assignment` has an optional `topic`.
3. Open `hub/pkg/prompts/agent/panel/v4/system.md`: the rules about the last request only, topic without assignment, «Быстрый черновик» and «Сделать под ключ» are present.
4. Versions v1-v3 directories are unchanged (`git diff origin/master -- paratech/hub/pkg/prompts/agent/panel/v1 .../v2 .../v3` is empty).
