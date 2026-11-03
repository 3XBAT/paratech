# TASK-374 — check steps (E2E Hub harness)

No UI: this is test infrastructure. Docker Desktop must be running.

1. In `paratech/e2e` run `npm ci`, then `npm test`: expect all tests green (the real-Hub test runs when Docker is up and is skipped otherwise).
2. With Docker stopped, `npm test` skips the Hub test and the rest stays green; `startHub` would report «Docker не запущен».
3. During the run `docker ps` shows a `paratech-e2e-postgres-1` container (project `paratech-e2e`); your own `hub-postgres-1` on :15432 and the Hub on :8080 are not touched. After the run only your own container remains.
4. If a run was killed with Ctrl+C: `docker compose -p paratech-e2e -f paratech/hub/docker-compose.yml down -v`.
5. LIVE mode needs the user env variables `PARATECH_E2E_ANTHROPIC_KEY` (+ `_BASE_URL`, `_AUTH`); without the key the harness stops with a typed «не проверено» error before any work.
