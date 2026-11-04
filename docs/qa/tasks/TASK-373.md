# TASK-373 — проверка владельцем: хуки режима E2E (только для разработки)

Обычный `paratech.exe` не меняется: хуков в нём нет вовсе (они собираются только с тегом `e2e`).

1. Соберите E2E-exe: `powershell -File paratech/desktop/build-alpha.ps1 -E2E -SkipWhisper -Smoke`. Должно быть «Smoke passed».
2. Проверка, что хуков нет в боевой сборке: `go -C paratech list -deps -tags production ./desktop/cmd/desktop` не содержит
   `adapters/e2ehooks`; с `-tags production,e2e` — содержит.
3. Запустите E2E-exe с пустыми временными папками (ваш paratech не трогайте): `--data-dir <tmp>\data --e2e-dir <tmp>\e2e`.
   Сначала студент видит пустое приложение «paratech · E2E».
4. Положите в `<tmp>\e2e\fixtures\` любой файл расписания и в `<tmp>\e2e\picks.jsonl` строку (с переводом строки в конце):
   `{"dialog":"schedule","paths":["fixtures/schedule.ics"]}`. В окне нажмите «Импортировать расписание»: диалог Windows не появляется,
   файл берётся из очереди; в `consumed.jsonl` появилась запись.
5. Строка с `"../x"` или абсолютным путём — импорт «отменён», в логе строка об ошибке.
6. Кнопка «Скопировать» в конспекте и «Открыть в Word» — браузер/Word не запускаются, в `<tmp>\e2e\opened.jsonl` появляются строки
   `{"kind":...,"value":...,"at":...}`. «Сохранить…» кладёт файл в `<tmp>\e2e\saved\`.
7. Закройте E2E-окно; удалите временные папки.
