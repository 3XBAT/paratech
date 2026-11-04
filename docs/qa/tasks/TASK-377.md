# TASK-377 — check steps (E2E settings pack)

No product UI changes. Docker must be running; the window «paratech · E2E» appears during the run (your own app keeps running).

1. First run: `bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @settings`. The model (57 MB) is downloaded once into `paratech/dist/cache/e2e` and checked against `catalog.json`; later runs reuse it (add `--no-build` to reuse the exe).
2. Open `paratech/dist/e2e-run/report/report.md`: ten rows (Hub: адрес, ключ и остаток; отозванный ключ; ключ прогона принят; Модель: испорченная копия, импорт с SHA-256, по умолчанию; Модуль расшифровки готов; Расписание: импорт, предметы, ссылка на пару), all «готово» today, 2-5 screenshots each in `report/screenshots/`.
3. Look at the screenshots «модели … после отказа / после установки»: the answer of the app is shown at the top of the page while the model row is far below (known UX finding, see `progress/TASK-377.md`).
4. `npm --prefix <abs>/paratech/e2e test` is green (340 tests).
5. After the run no `paratech.exe`/`hub.exe` of the run stays, `docker ps` shows no `paratech-e2e` container.
