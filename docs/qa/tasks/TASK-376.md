# TASK-376 — check steps (E2E layout pack)

No UI changes (only an invisible `data-status` attribute on status chips). Docker must be running; the window «paratech · E2E» appears during the run.

1. Normal size: `bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @layout`
2. Small size: `bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh env PARATECH_E2E_WINDOW=1024x640 npm --prefix <abs>/paratech/e2e run e2e -- --grep @layout` (add `--no-build` to reuse the exe).
3. Open `paratech/dist/e2e-run/report/report.md`: one row per screen and size «Раскладка · <экран> · <размер>». Today the pack reports real defects (see `progress/TASK-376.md`): «Сегодня» (кнопка «Добавить ссылку» обрезана), «Предметы» (имена без подсказки), «Билеты» (крошка сжата), «Задания» (422 в консоли), «Помощник» at the widest panel. Rows of the screens without data or the slow-model «Стоп» row are «не проверено», not «готово».
4. Each failed state has a screenshot «… нарушение» in `report/screenshots/`; the failure text names the screen, the state and the element.
5. `npm --prefix <abs>/paratech/e2e test` and the frontend `npm run test` / `run build` are green.
6. After the run no `paratech.exe`/`hub.exe` of the run stays (your own app keeps running), `docker compose ls` shows only your `hub` project.
