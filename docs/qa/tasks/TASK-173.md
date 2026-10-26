# QA TASK-173: desktop on catalog 3

Needs the Hub with TASK-172 deployed first.

1. Tests: `go -C <repo>/paratech test -race ./desktop/internal/domain/agent/... ./desktop/internal/cases/agentturn/... ./desktop/internal/input/... ./desktop/tests/...` and `npm --prefix <repo>/paratech/desktop/frontend run test` pass.
2. In the panel ask «Что горит на неделе?»: the reply is a short summary; the card data (checked in the log/DevTools event) carries `assignments` with titles, subjects and deadlines from the local base. The list is not drawn yet (TASK-176).
3. «Привет» and «Где посмотреть черновики?»: impersonal reply, no «ты/вы/привет», names «Задания» and the assignment page.
4. «Помоги с пр 2-4»: the panel looks up assignments and asks or proposes; no absurd partial card.
5. «Сделай презентацию на тему X» with no assignment: an honest short reply (topic work without an assignment is not supported yet), no card with a plan.
6. Optional live check: `PARATECH_AGENT_LIVE=1 go test -tags agentlive ./desktop/tests/agenteval/` (needs Hub and a tester key); report shows list and address metrics.
