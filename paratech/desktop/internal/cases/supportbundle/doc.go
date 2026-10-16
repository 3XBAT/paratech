// Package supportbundle is the use case of "Сохранить пакет для поддержки" on the diagnostics page (TASK-058, AC-4):
// on an explicit user action it writes a zip to the path the user chose with the app and Windows versions
// (about.json), the allowlist summary of the settings (settings.json, built by the settings aggregate), the recent
// capture diagnostics (capture.json) and the log files (logs/), with the user's profile directory replaced by
// %USERPROFILE%. It never adds the database, artifacts, audio, transcripts, summaries, homework, lesson or window
// titles, passwords or keys, and sends nothing anywhere.
//
// Rules that apply: ARCH-20..27.
package supportbundle
