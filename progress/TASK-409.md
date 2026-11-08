# TASK-409: «Расшифровать снова» also after a failed transcription

## Done
- `domain/transcription/services/retranscribe`: new `StateFailed` (finally failed, not for lack of speech). `Decide` allows a new job when the newest STT job is CANCELLED or FAILED (transcript and unfinished-job checks first). `StateFinished` now means completed or failed for lack of speech (refused). Key `stt:<rec>:<model>:r<n>` unchanged.
- `cases/transcribe/retranscribe.go`: `sttJobs` maps `StatusFailed` without `values.IsNoSpeech(LastError)` to `StateFailed`. `Retranscribe` itself is unchanged. Doc comments updated.
- `cases/activity/chain.go`: newest STT FAILED (not no-speech) = failed / transcriptionFailed / action `retranscribe` (was `openModels`). The no-speech and CANCELLED cases keep their order and results.
- Scenario `TestFailedTranscriptionStartsAgain` (desktop/tests, fake whisper-cli): final failure -> row transcriptionFailed + retranscribe -> restart sweep creates nothing -> Retranscribe (key r2, repeat refused) -> COMPLETED, transcript, summary/homework jobs -> a further Retranscribe refused. No-speech refusal and RETRY_WAIT are covered in unit tests.
- Frontend: no change (`retranscribe` action code already exists).

## Decisions to confirm
1. Only `StateFailed` added; no-speech maps to `StateFinished` (refused); a no-speech STT job also stores a transcript, so `hasTranscript` refuses it as well.
2. A FAILED row no longer offers `openModels`: `ActionOpenModels` is emitted nowhere now. When the model is missing, Retranscribe answers `no_model` and TASK-393 shows «Открыть модели».

## Follow-ups
- Until TASK-393 lands, a FAILED transcription row shows no action (`recordingRowView.ts` has no label for `retranscribe`). TASK-393 depends on this task.
- Reviewer medium (rare, deferred): if the completion event cannot be published on the last attempt (`ErrPublish`), the STT job ends FAILED with the transcript stored and no post-processing; the row then offers «Расшифровать снова» that always answers `not_allowed`. The chain should not offer it for that case (or a re-publish path should rerun the job via the `finishStored` path). The publish is a local SQLite enqueue, so this needs the retries to run out.
- The frontend items of the TASK-392 follow-ups (`not_allowed` after a double click = quiet refresh; the `ending` wording; stale line in `paratech/e2e/README.md`; `shell.go` comment) live in TASK-393 files and were not touched here; they stay with TASK-393.
- Nits: line wrap in `retranscribe/doc.go`.

## Changed existing tests
- `cases/activity/chain_test.go` (two cases), `chain_speech_test.go` (one): expected `ActionOpenModels`, now `ActionRetranscribe`.
- `retranscribe/retranscribe_test.go`: row «the newest failed» with `StateFinished` renamed «the newest completed or found no speech» (same input and result).
- `cases/transcribe/retranscribe_test.go`: the «failed» row left `TestRetranscribeRefuses` (now allowed); `autoJob` delegates to the new `autoJobFailing` with the same error text.
- `tests/retranscribe_sqlite_test.go`: setup of `TestCancelTranscriptionAndStartAgain` moved into the shared `newRetranscribeEnv`; behaviour unchanged.

## Verification
go vet and `GOOS=windows go vet ./desktop/...`, `go test -race` of domain/transcription, cases/activity, cases/transcribe, input/wails, desktop/tests (with the architecture test), golangci-lint v2.14.0 `./desktop/...` 0 issues.

## Review
reviewer (opus): approve (medium deferred above, lows). security-auditor (opus): no findings.

## Security
No new input: the recording id is validated as before; the decision reads the local job state; the user starts a job per click, nothing is reachable from the agent/LLM; no new logs of content.
