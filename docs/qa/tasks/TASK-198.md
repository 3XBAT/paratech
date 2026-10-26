# TASK-198 — check steps

The button is added in TASK-199; the core is checked by tests. From `paratech/`:

1. `go test -race ./desktop/internal/cases/turnkey/... ./desktop/internal/domain/coursework/... ./desktop/internal/adapters/pdf/... ./desktop/internal/input/... ./desktop/tests/...` pass.
2. `GOOS=windows go vet ./desktop/...` is clean.
3. Real check (after TASK-199): a done paper, «Скачать PDF»: the PDF appears in «ДЗ» of the subject next to the .docx, opens, Cyrillic is correct, title-page year equals the .docx. A second press does not create another file. Send to the teacher still attaches the .docx.
4. A computer without WebView2 shows «Не удалось сохранить PDF: на компьютере нет нужного компонента Windows (WebView2).»
