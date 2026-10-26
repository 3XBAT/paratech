# TASK-180: QA

Core only: the «Отправить преподавателю» button comes in a UI task (`agentConfirmApi.proposeSendWork`).

1. `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/domain/library/... ./desktop/internal/cases/homeworkflow/... ./desktop/internal/input/wails/... ./desktop/internal/app/... ./desktop/internal/adapters/sqlite/... ./desktop/tests/...` from `paratech/`: green.
2. Migration 0033: start the app on an existing profile; waiting and executed proposals are intact.
3. After the UI task: with a finished work of an assignment and a teacher address in settings, press send: a card with recipient, subject and the .docx/.pptx file name; nothing is sent before «Да».
4. Change the file in the library (replace/move it out of «ДЗ») after the card appears, press «Да»: no email, the proposal is stale.
5. Press «Да» twice: one email. A work from a file without an assignment: refusal «no_assignment», no card.
