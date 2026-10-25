# TASK-140 — проверка владельцем

Задача только про ядро; кнопки «Стоп» с новым поведением приходят в TASK-143. Проверка тестами:

1. `go test -race ./desktop/internal/cases/agentturn/... ./desktop/internal/input/...` — зелёные: `TestProgressBeforeTheHubStep`, `TestStopBetweenTwoActions`, `TestStopDuringARead`, `TestStopKeepsAPaidJob`, `TestStopDuringTheHubStep`, `TestContinueDuringAnActionCutByStop`.
2. `GOOS=windows go vet ./desktop/...` — без замечаний.
3. В обновлении панели есть поле `progress` (true до ответа Hub, без карточек).
4. Пока кнопки нет, вручную не проверяется.
