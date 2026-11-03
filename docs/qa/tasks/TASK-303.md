# TASK-303 — check steps (import core)

Use a small test audio file (WAV/MP3), not your real recordings. In the app, «Записи» -> «Импортировать запись».

1. Import the file once: the recording appears, transcription starts.
2. Import the same file again (any lecture): the same recording is returned («уже импортирована»), no second row; in the data folder `recordings/` there is exactly one `imp-<id>.<ext>` for it.
3. Orphan sweep (optional, with the app closed): copy any audio file into the data folder `recordings/` as `imp-zzzz.wav`, set it older than 10 minutes (or wait), start the app: the file is gone after start. A file named like a captured track (`<id>.wav`) or a fresh `imp-` file stays.
