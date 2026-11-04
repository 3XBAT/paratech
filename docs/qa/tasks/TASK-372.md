# TASK-372 — проверка владельцем: режим E2E (только для разработки)

Обычный `paratech.exe` не меняется: у него нет E2E-переключателей.

1. Из корня репозитория: `powershell -File paratech/desktop/build-alpha.ps1 -E2E -SkipWhisper -Smoke`. В конце должно быть «Smoke passed»;
   exe лежит в `paratech/dist/e2e/paratech.exe`.
2. Запустите обычный exe с `--e2e-dir x` — он должен ответить «flag provided but not defined: -e2e-dir» (код 2).
3. Оставьте свой paratech запущенным. Запустите E2E-exe с пустыми временными папками:
   `paratech.exe --data-dir <tmp>\data --e2e-dir <tmp>\e2e --e2e-cdp-port 39222 --e2e-window 1280x800`.
   Появится второе окно «paratech · E2E» (холодное приложение, без данных); ваше окно остаётся как было.
4. Откройте `http://127.0.0.1:39222/json/version` — ответ с версией браузера. Порт слушает только 127.0.0.1 (`netstat -ano`).
5. Запуск E2E-exe с вашим обычным каталогом данных должен отказать с понятной ошибкой.
6. Закройте E2E-окно. Ветка реестра `HKCU\Software\Classes\paratech` не изменилась (указывает на ваш exe).
