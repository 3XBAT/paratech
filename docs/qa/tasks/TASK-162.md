# TASK-162 — check steps (owner)

1. Open «Записи и задачи»: the button «Импортировать запись» is next to «Начать запись». The same button is on a subject page with the subject prefilled.
2. Open the panel, choose «Дата и время» (or a started lesson of today), pick the subject, press «Выбрать файл и импортировать»; the Windows file dialog opens. Pick an old WAV/MP3/FLAC/OGG (Vorbis). The note «Звук не покидает компьютер, распознаётся локально» is visible.
3. The row appears marked «импортирована» with the estimate; with the laptop on battery it shows the power wait. After a restart the mark is gone (known, see progress).
4. Refusals in Russian: an Opus/M4A file, a file over 2 GB, the same file twice, a file over 4 hours, a file whose length cannot be read.
5. Tab through the panel with the keyboard; the labels are read by a screen reader.
6. Tests: `npm --prefix paratech/desktop/frontend run test` and `run build`; `go test -race ./desktop/internal/domain/recording/... ./desktop/internal/cases/recordingimport/...`.
