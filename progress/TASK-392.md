# TASK-392: cancel transcription and «Расшифровать снова» — core and binding

## Done
- `domain/transcription/services/retranscribe` (pure): `Policy.Decide(rec, jobs, hasTranscript)` allows a new STT job only when there is no transcript, no unfinished STT job and the newest job is CANCELLED (newest = by created time, then the larger id); `Decision.Key(model)` = `stt:<rec>:<model>:r<n>`, n = number of STT jobs + 1 (same input, same key: a double click makes one job). Table test.
- `adapters/sqlite`: `ListJobsByKeyPrefix` (`substr` comparison, no LIKE, no wildcard issues), `Jobs.ListByKeyPrefix`; sqlc regenerated, no drift; no migration.
- `cases/transcribe`: `Enqueuer.ActiveJob`, `Enqueuer.Retranscribe` (one `Tx.Do`: `TranscribableAudio`, STT jobs, transcript, `ResolveForJob`, enqueue); errors `ErrNotAllowed`, `ErrNoModel`; port `TranscriptFinder`. The «one automatic STT job per recording» rule is unchanged, so a cancel survives a restart.
- `cases/activity`: newest STT job CANCELLED = stage `failed`, error `transcriptionCancelled`, action `retranscribe`; `CanCancel` while the newest STT job is not terminal.
- `input/wails/activity.go`: `CancelTranscription` (`cancelled | not_running | ending | failed`, through the existing `jobrunner.Service.Cancel`) and `Retranscribe` (`started | not_allowed | no_model | failed`); `RecordingItem.canCancel`.
- `app/core.go` wiring only (`recordingReruns` forwards to the enqueuer; `Forcer` gets `Cancel`).
- `frontend/src/bindings/activity.ts`: `canCancel`, code, action, result types, parsers, methods. No UI and no Russian texts (TASK-393).
- Review fix (reviewer medium): once the transcript is stored, `transcribe.Service.Handle` finishes the publish on `context.WithoutCancel`, so a cancel in that window yields COMPLETED with post-processing instead of a CANCELLED job with a transcript (where «Расшифровать снова» would answer `not_allowed`). Test fails without the fix.

## Decisions to confirm
1. `Decide` also takes the recording id (the key needs it); `Policy` method + `New()` per ARCH-17.
2. `shell.go` untouched (not in the allowed list): `JobForcer` gained `Cancel`, `PostProcessRetrier` gained `ActiveTranscription`/`Retranscribe`; the comment on `Core.Retrier` in `shell.go` is now incomplete.
3. The list query returns full job rows (reuses `jobFromRow`), not only id/status/created.
4. A cancel before the transcript is saved: CANCELLED, «Расшифровать снова» works. After it is saved: the job completes (see above).

## Follow-ups
- TASK-393: until it lands a CANCELLED row has no text/action in `recordingRowView.ts` (empty hint; the state is reachable only through jobrunner recovery cancels). `ending` is also returned for a job claimed a moment ago whose handler has not started: the text should invite another try; `not_allowed` after a double click should be a quiet refresh. Update the stale line in `paratech/e2e/README.md` («no «Отменить» for a running STT job»).
- Cancel between writing the whisper document and saving the transcript leaves the cancelled job's document artifact on disk (not audio); cleanup is a follow-up.
- The agent has no tool yet (`cancel_transcription`, `retranscribe`), row in `docs/agent-parity.md` in TASK-393.
- A FAILED transcription still has no «Расшифровать снова» (owner question, per the draft).

## Changed existing tests
- `cases/activity/chain_test.go`: the old cancelled case expected `transcriptionFailed`/`openModels`, now `transcriptionCancelled`/`retranscribe`; queued/waiting/retry/running/newest cases expect `CanCancel: true`. `chain_speech_test.go`, `service_test.go`: one running-transcription expectation each gets `CanCancel: true`.
- `input/wails/activity_test.go`: list JSON includes `canCancel`; `settings_test.go`: bound-method count 9 -> 11.
- `cases/transcribe/fakes_test.go`: `fakeEvents.TranscriptCreated` fails on a done context (like a real enqueue); `enqueue_test.go` fakes extended.
- Frontend: `test/activity.test.mjs`, `advice.test.mjs` (method count 9 -> 11), `lecturetext.test.mjs` get `canCancel`; `src/recordingRowView.ts` type-only `Exclude<>` additions (outside the allowed list, minimal compile fix). `desktop/tests/transcribe_sqlite_test.go`: optional `events` publisher in `sttOptions`.

## Verification
go vet and `GOOS=windows`-native vet (dev machine), `go test -race` of domain/transcription, cases/transcribe, cases/activity, adapters/sqlite, input/wails, app, desktop/tests (incl. arch test and the new scenario `TestCancelTranscriptionAndStartAgain`: hanging fake whisper-cli, cancel, restart, again to post-processing), golangci-lint v2.14.0 `./desktop/...` 0 issues, sqlc generate no drift, frontend `npm run test` (2245 pass) and `run build`.

## Review
reviewer (opus): changes requested (medium: cancel after the transcript is stored leaves a dead end; lows) -> fixed in 8b418308 (small, with a failing-without-fix test; no further re-review). security-auditor (opus): no findings.

## Security
No new inputs beyond a validated recording id on two Wails bindings; prefix query uses placeholders and `substr` (no LIKE); no job id comes from the client; cancel goes through the existing job-object kill; not reachable from the agent/LLM; no secrets in logs.
