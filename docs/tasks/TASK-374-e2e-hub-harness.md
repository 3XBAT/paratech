# TASK-374: E2E Hub harness and the scripted fake Messages API

- **Agent:** hub-dev (opus)
- **Label:** test
- **Depends on:** none (runs in parallel with TASK-372)
- **PRD sections / AC:** ADR-0035 §1, §4; ADR-0010, ADR-0023; PRD §12; AC-11, AC-25
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-374

## Goal
One TypeScript module starts a throwaway real Hub (Postgres schema, test key, free port) in front of either a scripted
fake Anthropic Messages API or the real provider, and tears everything down.

## Allowed to change
- `paratech/e2e/{package.json,package-lock.json,tsconfig.json,.gitignore}` (new package: `typescript`, `@types/node`,
  pinned exact; `npm test` = `tsc` + `node --test`)
- `paratech/e2e/src/hub/**`, `paratech/e2e/src/fakellm/**`, `paratech/e2e/test/**`
- `paratech/e2e/README.md` (how to run the harness alone)

## Out of scope
- Playwright and the app (TASK-375); any Go change in `hub/` (if the harness needs one, stop and report).

## Acceptance criteria
- [ ] `startHub({mode})`: checks Docker and starts the `postgres` service of `hub/docker-compose.yml` (respects
      `HUB_PG_PORT`) or uses `HUB_TEST_DATABASE_URL`; creates schema `e2e_<run>`; builds `hub.exe` once into
      `paratech/dist/e2e/`; `hub keys create -label e2e-<run>` with `search_path`; `hub serve` on a free loopback port
      (refuses 8080 and 58080), random `HUB_AGENT_SEAL_KEYS`, `HUB_QUOTA_LIMIT` from the caller (default 400000),
      `HUB_PROVIDER_STREAM=false` in scripted mode; waits for `/readyz`. Returns `{url, apiKey, stop()}`; `stop` ends the
      Hub and drops the schema. Environment errors are typed («Docker не запущен»), not feature failures.
- [ ] Scripted mode: dummy provider key, `HUB_ANTHROPIC_BASE_URL` = the fake. Live mode: key, base URL and auth from
      `PARATECH_E2E_ANTHROPIC_KEY`, `PARATECH_E2E_ANTHROPIC_BASE_URL`, `PARATECH_E2E_ANTHROPIC_AUTH` (missing key = typed
      error); the key is passed only in the Hub's environment and never printed (test greps the harness output).
- [ ] Fake Messages API (`node:http`, loopback): rules `{kind: "tools"|"plain", match, reply, usage}` consumed in order;
      `POST /__e2e/script` sets the script, `GET /__e2e/requests` returns recorded request bodies, `GET /__e2e/unmatched`
      the unmatched ones (each answered HTTP 500 `api_error`). Response shape (content blocks, `tool_use`, `stop_reason`,
      `usage`) follows `hub/internal/adapters/anthropic` and `docs/refs/anthropic.md`.
- [ ] Unit tests (`node --test`, no Docker): rule matching and order, unmatched handling, response shape; one test
      that starts the real Hub against the fake and runs `GetRemaining` over Connect JSON is skipped without Docker.
- [ ] `npm test` passes; the Windows run of the Docker test is recorded in progress.

## Notes
`hub/testenv.FakeMessagesAPI` is the Go reference for the wire format; it is not reused (Go test package).
