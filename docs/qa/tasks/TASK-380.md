# TASK-380 — check steps (E2E pack «Агент»)

Nothing to click in the app: this task adds tests. Docker Desktop must be running (start it yourself).

1. Run the pack (settings and recording first, they prepare the data; about 5 minutes):
   `bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <repo>/paratech/e2e run e2e -- --project=settings --project=recording --project=agent`
   (add `--no-build` to reuse the built exe). A window «paratech · E2E» opens; do not touch it.
2. Open `paratech/dist/e2e-run/report/report.md`. Rows «Агент: …»: 13 «готово», 5 «сломано» on the current build:
   - «карточки инструментов остаются после ответа модели» — answer, confirm and «Отменить» cards vanish when the model replies;
   - «текст ответа в карточке» — «что было на паре» shows no answer text, only the sources;
   - «второй вопрос, пока идёт инструмент» — after «Отправить ещё раз» the refusal line stays;
   - «после оборванного ответа можно писать дальше» — «Нет связи с paratech» and a disabled field after a cut reply;
   - ««Отменить» обновляет открытое дерево предметов» — the open tree keeps the subject.
   Each red row has a screenshot link; these are app defects to fix, not test faults. When one is fixed its row turns «готово».
3. By hand (cold app) you can see D1: ask the panel «удали запись про перезапуск»; the confirm card shows, then disappears when the reply text arrives.
4. The model's own wording (does it invent a cause, does it send you to «Расписание») is not checked here: that is the LIVE set (TASK-382).
