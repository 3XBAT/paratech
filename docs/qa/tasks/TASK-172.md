# QA TASK-172: planner panel v3 (Hub)

The desktop still sends catalog 2 until TASK-173, so on the live gateway it gets the v2 prompt; the four questions below
check v3 once TASK-173 is in. Before that, steps 1-3 can be checked.

1. Tests: `go -C <repo>/paratech test -race ./hub/pkg/prompts/... ./hub/internal/domain/llm/... ./hub/internal/adapters/anthropic/... ./hub/internal/cases/agentstep/... ./hub/config/...` and `go -C <repo>/paratech/shared test ./...` pass.
2. Hub start: `HUB_AGENT_SEAL_KEYS` is set (README, «Hub agent step»: `openssl rand -hex 32`, then `1:<hex>`). Without it every
   panel step shows «hub rejected the request».
3. Hub log: every `agent step` line has `stop_reason=` (`tool_use` for a normal step). A step cut at the cap shows
   `stop_reason=max_tokens` and `err="output cap"`, not `no tool use`; texts are never in the log.
4. In the panel (desktop on catalog 3), ask the beta questions:
   - «Привет» — a reply without «Привет», without «ты»/«вы»/«я».
   - «Где посмотреть черновики?» — a reply naming «Задания», the page of the assignment, block «Решение»; no
     «Целиком не получится» card, no advice to add subjects.
   - «Сделай презентацию на тему X» — no «на вашей стороне — создать презентацию» item: the panel starts (or proposes) the
     work from an assignment, or asks which assignment it is; if a «Целиком не получится» card appears, its items do not
     contradict each other and the app's part lists only what the app does.
   - «Что горит на неделе?» — a reply with a short summary (how many, nearest deadline) and the list of the assignments with
     their titles and deadlines (titles come from the desktop).
