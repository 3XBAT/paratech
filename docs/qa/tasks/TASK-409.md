# TASK-409: check steps (owner)

The task is the core only; the button and its texts come in TASK-393.

1. Tests, from `paratech/`: `go test -race ./desktop/internal/domain/transcription/... ./desktop/internal/cases/transcribe/... ./desktop/internal/cases/activity/... ./desktop/tests/...` — all green. The scenario `TestFailedTranscriptionStartsAgain` plays it with a fake whisper-cli: recognition fails until attempts run out, the recording row reports a failed transcription with the action «start again», a restart creates nothing, «start again» makes a new job that runs through to the summary, and a second call is refused.
2. Cold app: nothing new on screen. A failed transcription row has no action button until TASK-393 (it no longer offers «Открыть модели»; TASK-393 brings «Расшифровать снова» and «Открыть модели» for a missing model).
3. A recording without speech still shows «Проверить звук» and is never offered a new recognition.
4. The user-visible check (button after a failure) is in docs/qa/tasks/TASK-393.md.
