# TASK-161 — check steps (owner)

The core has no UI yet (TASK-162), so the check is by tests.

1. Run `go test -race ./desktop/internal/app/ -run RecordingImport` from `paratech/`. It imports a synthetic WAV, checks the copy is byte-identical, the STT job is created with the pinned model, no Hub request carries the audio, and the same file twice gives one recording.
2. In the same test, deleting the recording removes only the copy in storage; the temp original keeps its bytes.
3. `go test -race ./desktop/internal/domain/recording/... ./desktop/internal/cases/recordingimport/...` for formats (WAV, FLAC, MP3, Ogg Vorbis accepted; Opus, M4A, WMA, AAC refused) and limits.
4. Decide the open questions in `progress/TASK-161.md` (limits, video/M4A).
