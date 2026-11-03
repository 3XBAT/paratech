# TASK-375: Playwright runner: sandbox lifecycle, CDP attach, report

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-372, TASK-373, TASK-374
- **PRD sections / AC:** ADR-0035 §2, §3, §6, §7
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-375

## Goal
`npm run e2e` builds (or reuses) the E2E exe, starts the Hub harness and the app in a fresh sandbox, attaches
Playwright over CDP to both windows, runs the specs and writes the «готово / сломано» report.

## Allowed to change
- `paratech/e2e/{package.json,package-lock.json,playwright.config.ts,tsconfig.json,.gitignore,README.md}`
  (add `@playwright/test`, exact version after `npm view @playwright/test version`; no browser download)
- `paratech/e2e/src/{sandbox,app,report,steps}/**`, `paratech/e2e/specs/smoke/**`, `paratech/e2e/test/**`
- `.github/workflows/ci.yml` (job `e2e-static`: `npm ci`, `npm test`, `tsc --noEmit` in `paratech/e2e`; no run)
- `docs/process/e2e.md` (how to run, add a scenario, read the report), `docs/refs/playwright-webview2.md`

## Out of scope
- Feature scenarios (TASK-376…381); the live set (TASK-382).

## Acceptance criteria
- [ ] Global setup: sandbox root `paratech/dist/e2e-run/` wiped; `<e2e-dir>` = `…/ctl`, data dir `…/ctl/data`; fixtures
      copied to `<e2e-dir>/fixtures`; free ports for CDP, Hub, fake; Hub harness started (TASK-374); the exe started
      with `--data-dir --e2e-dir --e2e-cdp-port --log-level debug`; waits for `instance lock acquired`,
      `secrets_profile=isolated`, `e2e mode on`, `background work started` in `<data>/logs/desktop.log` (else abort:
      «песочница не изолирована»); `connectOverCDP`; main page = URL with `index.html` or `/`, popup = `popup.html`.
- [ ] Teardown: hard stop of the exe (as `build-alpha.ps1 -Smoke`), Hub `stop()`, deletion of the Credential Manager
      entries `paratech-<secrets_profile_id>/…` with `cmdkey /list` + `cmdkey /delete` (never another prefix), report.
- [ ] Helpers: `pick(dialog, paths)` writes `picks.jsonl`; `opened()` reads `opened.jsonl`; `llm.script(rules)` and
      `llm.requests()`; `step(name, fn)` = `test.step` with a screenshot on failure.
- [ ] Reporter writes `paratech/dist/e2e-run/report/report.md`: table «Функция | Режим | Итог | Шаг, где сломалось |
      Снимок» (feature = spec annotation `feature`), «не проверено (нужно: …)» for skipped dependents, run time,
      environment errors separately; the Hub key and any `PARATECH_E2E_*` value never appear (test).
- [ ] `workers: 1`, `retries: 0` (scripted), traces only on failure; budget check prints a warning over 15 min.
- [ ] Smoke spec: both pages found; main window screenshot in the report; `opened.jsonl` empty; one row «Запуск
      приложения — готово».
- [ ] `docs/refs/playwright-webview2.md` records what was verified: CDP attach to WebView2 155+, popup target while
      hidden, screenshot of an occluded/minimized window (works or not), Chromium debug port bound to loopback.
- [ ] `bash heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @smoke` passes on the dev machine (log in
      progress); `npm test` passes; CI job green.

## Notes
One command, run through `paratech-ops/heavy.sh` (it holds the machine lock for the whole run). `--no-build` reuses
`paratech/dist/e2e/paratech.exe`.
