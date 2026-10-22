# TASK-114: check steps

No user-visible change. Hub only; the desktop does not call AgentStep yet.

1. From `paratech/`: `go test -race ./hub/... ./shared/...` is green.
2. Postgres tests: start `docker compose -f hub/docker-compose.yml up -d --wait postgres` (set `HUB_PG_PORT` if 55432 is reserved), run `go test -race ./hub/internal/adapters/postgres/... ./hub/tests/...` with `HUB_TEST_DATABASE_URL` set, then `docker compose ... down -v`.
3. Without `HUB_AGENT_SEAL_KEYS` the Hub logs a warning and `AgentStep` answers unimplemented; with a key (`ver:hex`, 32+ bytes) it is served. The planner alias must be in `HUB_LLM_MODELS`.
