# QA TASK-131: write_work

The UI for this arrives with TASK-132; until then check by tests.

1. Из paratech/: `go test -race -count=1 ./desktop/internal/cases/turnkey/... ./desktop/internal/domain/coursework/... ./desktop/tests/...`
2. Сценарий `desktop/tests/turnkey_writing_test.go`: план -> «Утвердить план» -> разделы по шагу очереди -> .docx в «ДЗ» предмета; регенерация возвращает прежний файл во «Входящие»; отмена действия агента возвращает файл во «Входящие».
3. [владельцу, после TASK-132] попросить агента написать реферат по подтверждённому заданию: появляется план, правка, «Утвердить план», прогресс «Раздел N из M», файл открывается в Word, оформление по ГОСТ, есть пометка «Сгенерировано — прочитайте перед сдачей».
