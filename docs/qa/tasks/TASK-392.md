# TASK-392: check steps (owner)

The task is the core only; the button and texts come in TASK-393. What can be checked now:

1. Tests: `go test -race ./desktop/internal/domain/transcription/... ./desktop/internal/cases/transcribe/... ./desktop/internal/cases/activity/... ./desktop/internal/input/wails/... ./desktop/tests/...` from `paratech/` — all green. The scenario `TestCancelTranscriptionAndStartAgain` plays the whole story with a fake whisper-cli: recognition runs, is cancelled (the job ends CANCELLED, the work folder is gone, no summary and no homework jobs), a restart makes no new job, «start again» creates a new job that runs to the summary.
2. Cold app: nothing changes on screen. Recording → «⋯ → Удалить» during recognition still stops whisper-cli (the old path, unchanged); the E2E recording pack stays green.
3. The user-visible check (cancel icon, «Расшифровка отменена», «Расшифровать снова», whisper-cli gone from the task manager) is in docs/qa/tasks/TASK-393.md.
