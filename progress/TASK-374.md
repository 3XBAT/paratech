# TASK-374 — E2E Hub harness and the scripted fake Messages API

Branch ralph/20261104-task-374. New package `paratech/e2e` (TypeScript, no Playwright yet). No Go change in `hub/`.

## What was done
- `startHub({mode, quota})` (`src/hub/`): own compose project `paratech-e2e` (or `HUB_TEST_DATABASE_URL`), schema `e2e_<run>`,
  `hub.exe` built once into `paratech/dist/e2e/`, `hub keys create -label e2e-<run>`, `hub serve` on a free loopback port
  (8080 and 58080 refused), random seal keys, quota default 400000, `HUB_PROVIDER_STREAM=false` in scripted mode, bounded
  `/readyz` poll. Returns `{url, apiKey, stop()}`; `stop` is idempotent and also runs after a failed start (kill Hub tree,
  drop schema, stop the compose stack only if this process started it).
- Fake Messages API (`src/fakellm/`): rules consumed as «first remaining match» on the last user content; control endpoints
  `POST /__e2e/script`, `GET /__e2e/requests`, `GET /__e2e/unmatched`; unmatched -> HTTP 500 `api_error`; unknown paths are
  recorded too; wire format per `hub/internal/adapters/anthropic`.
- Errors: `E2EEnvironmentError` (Docker, port, live key, config, DB, go toolchain missing -> «не проверено») vs `HubFailedError`
  (build, key creation, not ready -> «сломано»). `isEnvironmentError(e)` for the future runner.
- Own Postgres client (`pg.ts`, simple protocol, SCRAM-SHA-256/MD5, no TLS) to avoid a dependency; checked against RFC 7677.
- Live key handling: only in the spawned `hub serve` env; Hub env is an allowlist; go/docker get the environment minus
  `HUB_*`/`PARATECH_*`; captured output scrubbed; returned `apiKey` is non-enumerable and masked.
- ADR-0035: status Accepted, owner answers О1-О6 (04.11) recorded (the supervisor passed them as the owner's answers).

## Windows Docker run (AC)
`bash heavy.sh env PARATECH_E2E_PG_PORT=25432 npm --prefix .../paratech/e2e test` (Windows 10, Docker 29.8.2, Node 22.11):
50/50 pass, 0 skipped; real Hub test ready in 7.3 s, 8.5 s total (earlier run before review fixes: 46/46, ready 5.1 s).
Also verified: reuse of an already running E2E postgres (left running), and the `HUB_TEST_DATABASE_URL` path (SCRAM login).
Afterwards `docker ps -a` shows only the owner's `hub-postgres-1`; no `e2e_*` schemas left.

## Decisions to confirm
- `HUB_PG_PORT` is read as a fallback (`PARATECH_E2E_PG_PORT` > `HUB_PG_PORT` > 55432) and `HUB_TEST_DATABASE_URL` is honoured, as
  the task text says. If the owner's environment points them at the owner's dev Postgres (:15432), the harness would use that
  port (fails typed if busy) or create/drop only its own `e2e_<run>` schema there. Alternative: read only `PARATECH_E2E_*`.
  55432 may be reserved by Hyper-V on some machines (then typed PortBusy; set `PARATECH_E2E_PG_PORT`).
- A non-ENOENT spawn failure of `go` is also reported as environment (GoToolchainMissing).
- Fake rules: «first remaining match», not strict head-of-list.

## Follow-ups
- Wire `paratech/e2e` into Linux CI (`npm ci`, `npm test`) with `PARATECH_E2E_UNIT_ONLY=1` (the Docker test would otherwise
  run on runners with Docker); `.github` is outside this task.
- The fake has no thinking blocks (SCRIPTED checks the app side); a 500 for unmatched calls is transient for the Hub, so the
  desktop queue may retry: TASK-375 should fail the scenario on the first unmatched record.
- Ctrl+C of a runner does not trigger the exit guard (SIGINT); README documents `docker compose -p paratech-e2e down -v`.
- Fake control endpoints have no Host/Origin check (auditor, below the bar: synthetic data only).

## Review
reviewer: approve with conditions (progress file, ADR attribution, error classification, env variables) - fixed or recorded
above (commit 21d90af2); no re-review needed (no blocker/high). security-auditor: no findings.

## Changed existing tests
None (new package).

## Security line
No secrets committed; live key only in the Hub process env; no Go/CI/config changes; owner's Hub/DB/compose project untouched.
