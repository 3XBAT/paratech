# TASK-375: Playwright runner (sandbox, CDP, report)

## Done
- `paratech/e2e`: `@playwright/test` 1.64.1 pinned (no browser download). `npm run e2e [-- --grep @tag] [--no-build]`, `e2e:live` (plumbing only, not run).
- `src/sandbox` (wipe guard on `dist/e2e-run`, ctl/data/fixtures, free ports, build via `build-alpha.ps1 -E2E`, Hub + fake LLM, teardown: hard stop by pid, Hub, fake, Credential Manager entries of `paratech-<id>/` only), `src/app` (launch with minimal env, four log lines else «песочница не изолирована», loopback-only netstat check, `attachApp`), `src/steps` (`test`, `step`, `pick`, `opened`, `llm`, clipboard stub, first unmatched provider record fails the scenario), `src/report` (report.md «Функция | Режим | Итог | Шаг, где сломалось | Снимок», secrets scrubbed, environment problems «не проверено»), `specs/smoke`.
- Harness reads only `PARATECH_E2E_*` inputs (HUB_PG_PORT / HUB_TEST_DATABASE_URL reads removed). Postgres port: `PARATECH_E2E_PG_PORT`, else a free loopback port (55432 lies in this machine's Windows excluded range 55361-55460).
- Docs: `docs/process/e2e.md`, `docs/refs/playwright-webview2.md`, README of `paratech/e2e`. CI job `e2e-static` (npm ci, npm test with UNIT_ONLY, tsc --noEmit).

## Real run (this machine, 04.11)
- `heavy.sh npm run e2e -- --grep @smoke` with the build (56 s) and `--no-build` (9 s): 1 passed, row «Запуск приложения | SCRIPTED | готово». No leftover paratech.exe/hub.exe/compose project/Credential Manager entry after the run.
- `npm test` with Docker: 209 pass, 0 fail.
- WebView2 156.0.4314.58: attach works; popup target exists at start but its screenshot times out while hidden; main window screenshot works when covered or `SW_HIDE`, NOT when minimized or closed to the tray (restore first); CDP bound to 127.0.0.1 only (owned by an msedgewebview2 child). Details in `docs/refs/playwright-webview2.md`.

## Decisions to confirm
- Budget (15 min) is counted from the start of tests; the exe build is not counted; `globalTimeout` 45 min. LIVE warning at 30 min (ADR §6).
- Pick validation is stricter than the app (no backslashes, one path except `notes`).
- Out-of-list files touched: `src/cli.ts`, `src/contract.ts`, `src/hub/*` (supervisor rule), `tsconfig.json` (+DOM lib).

## Follow-ups
- Gap (low): if the teardown runs before the app spawn and `launchApp` then rejects, the catch's memoized `stop()` does not stop an app registered by `onSpawn` afterwards; only the exit guard covers it at process exit.
- Traces/screenshots may hold text typed into the UI (the throwaway Hub key): not scrubbed, stay under `paratech/dist`.
- Playwright's list reporter prints setup error texts unscrubbed (they rely on the harness scrubbing its own messages).
- Popup screenshot of a window the app really showed, clipboard write result, other DPI/monitors: not verified (refs doc).
- The beta exe is also named `paratech.exe`: the runner kills only by its own pid, never by name.
- Hub `AGENT` scenarios (TASK-376..381) can use `step`, `pick`, `llm`; layout checks need the minimized-window restore rule.

## Changed existing tests
`hub-harness.test.ts` (composePort ignores HUB_PG_PORT, async free port), `hub.integration.test.ts` (Docker only, no URL shortcut).

## Security
Security auditor: no high/medium; lows (pid reuse kill, CDP listener identity race, traces unscrubbed, fake Host guard) — pid reuse and Host guard fixed, the rest documented.
