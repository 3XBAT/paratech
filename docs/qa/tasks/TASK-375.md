# TASK-375 — check steps (E2E runner)

No UI: test infrastructure. Docker Desktop must be running; a paratech window titled «paratech · E2E» may flash on screen during a run.

1. Run `bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @smoke` (add `--no-build` to reuse `paratech/dist/e2e/paratech.exe`; the first run builds it).
2. Open `paratech/dist/e2e-run/report/report.md`: one row «Запуск приложения | SCRIPTED | готово» with a link to a screenshot of the main window (the «Сегодня» screen).
3. After the run: `tasklist` shows only your own paratech.exe / hub.exe, `docker compose ls` shows only your `hub` project, `cmdkey /list` has no new `paratech-…` entries. Your app, data, Hub on :8080 and Postgres :15432 were not touched.
4. `npm --prefix <abs>/paratech/e2e test` is green.
5. After a Ctrl+C: kill the leftover E2E paratech.exe / hub.exe (by pid) and run `docker compose -p paratech-e2e -f paratech/hub/docker-compose.yml down -v`.
6. How to add a scenario: `docs/process/e2e.md`.
