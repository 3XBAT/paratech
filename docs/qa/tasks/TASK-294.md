# TASK-294 — check steps (owner)

1. Open «Настройки». There is a group «Конспекты» with the switch «ИИ распознаёт фото конспектов», OFF on a new profile, with the text «Распознаёт Windows на этом компьютере, фото никуда не отправляются. Рукописный текст распознаётся хуже печатного.»
2. On a database from before this task (your current profile) the switch is also OFF after the update.
3. Turn it on, close and reopen the app: it stays on. Turn it off: it stays off.
4. If Windows has no Russian text recognition (Параметры → Время и язык → Язык → Русский → Распознавание текста), with the switch on a hint says how to add it. With the language installed, no hint.
5. Nothing is recognised yet: the setting only stores the choice (recognition comes with TASK-167).
6. Tests: `go test -race ./desktop/internal/domain/settings/... ./desktop/internal/cases/preferences/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/input/wails/...`; `npm --prefix paratech/desktop/frontend run test` and `run build`.
