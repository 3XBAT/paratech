# TASK-116: owner check

Core only, nothing visible in the app yet (no panel, no Driver). Nothing to check by hand.
CI covers desktop tests. Hub Postgres tests: `docker compose -f hub/docker-compose.yml up -d --wait postgres`, then `go test -race ./hub/internal/adapters/postgres/... ./hub/tests/...` with `HUB_TEST_DATABASE_URL`, then `docker compose ... down -v`.
