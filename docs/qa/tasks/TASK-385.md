# TASK-385: бета-сборка с hub.exe (`build-alpha.ps1 -Beta`)

Это сборка, не функция приложения: студент ничего не увидит, в окне беты пока тоже ничего нового (локальный Hub подключат TASK-387/389). Проверка владельца:

1. Откройте новую PowerShell в корне репозитория. Если бета сейчас запущена, соберите в другую папку, чтобы не перезаписать её exe:
   `powershell -NoProfile -ExecutionPolicy Bypass -File .\paratech\desktop\build-alpha.ps1 -Beta -Smoke -OutDir $env:USERPROFILE\pt-beta-check`
   (без `-OutDir` — в `paratech\dist\beta`).
2. В конце должны быть строки `Built ...\paratech.exe`, `Built ...\hub.exe`, `Smoke passed: the app started` и `Smoke passed: hub.exe -h printed its usage.` В папке лежат оба exe. Окон нет, звука нет, Hub не запускается (в диспетчере задач после скрипта нет `hub.exe`).
3. `go -C paratech version -m <папка>\paratech.exe` показывает `-tags=production,beta`; для `hub.exe` — `CGO_ENABLED=0`.
4. Обычная сборка в ту же папку: `... build-alpha.ps1 -Smoke -OutDir $env:USERPROFILE\pt-beta-check` — скрипт пишет `Removing ...\hub.exe of an earlier -Beta build` и `Symbols: N in the exe, none of cases/localhub or adapters/hubprocess`; `hub.exe` в папке больше нет.
5. `... build-alpha.ps1 -Beta -E2E` и `... -Beta -ComponentOnly` сразу завершаются понятной ошибкой.
6. Папку проверки потом можно удалить.
