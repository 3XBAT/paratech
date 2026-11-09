# TASK-401: проверка владельцем

Это тесты, не изменение приложения. Проверка:
1. Docker Desktop запущен.
2. Сборка: `bash paratech-ops/heavy.sh powershell -NoProfile -ExecutionPolicy Bypass -File paratech/desktop/build-alpha.ps1 -E2E`.
3. Прогон: `bash paratech-ops/heavy.sh npm --prefix paratech/e2e run e2e -- --no-build --project settings --project recording --project agent`.
4. Ожидается: все строки «готово», кроме одной «сломано» — «Агент: «Отменить» обновляет открытое дерево предметов» (D5, отложено).
5. Новые строки: «Агент: отмена ждущего вопроса — текст возвращается в поле», «Агент: один обрыв — одна автоповторная попытка под тем же ключом»; в «Агент: оборванный ответ сохраняет текст» в заметках видны ключи шага и новый ключ после «Повторить» (`…:r1`).
