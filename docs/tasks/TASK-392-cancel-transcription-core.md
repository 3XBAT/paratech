# TASK-392: «Отменить расшифровку» and «Расшифровать снова»: core and binding

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** none
- **PRD sections / AC:** §4.3 (STT sidecar, cancel = kill the process), §7.2 (QUEUED/RUNNING → CANCELLED by the user),
  §8; owner 2026-11-08
- **Агент:** пока нет инструмента (follow-up: `cancel_transcription` class U without compensation, as «останови
  запись» О-7 а; `retranscribe` class U); row in `docs/agent-parity.md` in TASK-393
- **Branch:** ralph/<date>-task-392

## Goal
While a recording is recognised (or waits for it), the student can cancel; whisper-cli stops, the recording stays,
its row says the recognition was cancelled and offers to start it again.

## Existing machinery (reuse, do not duplicate)
- `cases/jobrunner/service.go` `Service.Cancel(ctx, jobID)`: a waiting job → CANCELLED at once; a running one gets its
  handler context cancelled → CANCELLED when the handler returns (`ErrNotCancellable` otherwise).
- `adapters/whispercli/proc_windows.go`: the process runs in a job object, `TerminateJobObject` on context end
  (TASK-378 saw `type=stt outcome=cancelled` on recording delete); `proc_other.go` likewise.
- `cases/recordingdelete/ports.go` `JobCanceller` (the delete path already cancels STT jobs this way).
- `cases/transcribe/enqueue.go`: key `stt:<recording>:<model>`; «one automatic STT job per recording» — the startup
  sweep and `RecordingCompleted` create nothing while any STT job exists, so a cancel sticks across restarts.
- `cases/activity/chain.go` (`failedJob` counts CANCELLED as failed today), `input/wails/activity.go`
  (`RecordingItem`, `ActivityService`), job states `domain/jobs/job/transitions.go` (CANCELLED is final).

## Allowed to change
- `paratech/desktop/internal/domain/transcription/services/retranscribe/*` (new, pure)
- `paratech/desktop/internal/cases/transcribe/*` (`ActiveJob(ctx, recording)`, `Retranscribe(ctx, recording)`, ports)
- `paratech/desktop/internal/cases/activity/chain.go`, `types.go` (+ tests)
- `paratech/desktop/internal/adapters/sqlite/jobs*.go`, `queries/jobs.sql`, `sqlcgen/*` (list STT jobs of a
  recording by key prefix: id, status, created; no migration)
- `paratech/desktop/internal/input/wails/activity.go` (+ tests), `paratech/desktop/internal/app/*` (only wiring)
- `paratech/desktop/frontend/src/bindings/activity.ts` (new fields, codes and methods; texts in TASK-393)
- `paratech/desktop/tests/*` (+ tests)

## Acceptance criteria
- [ ] `retranscribe.Decide(sttJobs, hasTranscript)`: allowed only when no transcript exists, no STT job of the
      recording is unfinished and the newest one is CANCELLED; returns the next key `stt:<rec>:<model>:r<n>` with
      n = number of STT jobs + 1 (same input → same key, so a double click makes one job). Table test.
- [ ] `transcribe.Enqueuer.Retranscribe`: in one `Tx.Do` re-reads the recording (`TranscribableAudio`), the jobs and
      the transcript, pins the current model (`ResolveForJob`), enqueues; no model → known error `no_model`.
- [ ] `ActivityService.CancelTranscription(recordingId)`: finds the unfinished STT job of the recording
      (`transcribe.ActiveJob`) and calls `jobrunner.Cancel`; results `cancelled` | `not_running` (nothing to cancel) |
      `ending` (`ErrNotCancellable`: the attempt is ending, the row refreshes) | `failed`. Repeat is safe.
- [ ] `ActivityService.Retranscribe(recordingId)` → `started` | `not_allowed` | `no_model` | `failed`.
- [ ] Chain: a CANCELLED newest STT job → stage `failed`, error code `transcriptionCancelled`, action `retranscribe`
      (not `transcriptionFailed`); `RecordingItem.CanCancel` true while the newest STT job is QUEUED, waiting or
      RUNNING; the chain always reads the newest STT job of the recording.
- [ ] After a cancel: the work dir of the job is gone (whispercli cleanup, else the startup cleanup) and no
      `summarize`/`extract_homework` is enqueued; restart of the app → the row still says cancelled (sweep makes no job).
- [ ] Tests: use case on fakes; sqlite list query; chain table; binding codes; scenario in `desktop/tests` with the
      fake whisper-cli: start, cancel running → CANCELLED and the process gone, «Расшифровать снова» → new job runs to
      COMPLETED and the post-processing follows.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of the touched scope and `desktop/tests` (Windows too: the
      process kill), arch test, golangci-lint, sqlc generate without drift.

## Notes
Recording deletion keeps its own cancel path (unchanged). A FAILED transcription does not get «Расшифровать снова» in
this task (owner question).
</content>
</invoke>
