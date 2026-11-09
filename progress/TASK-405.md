# TASK-405 — «Подключиться» while the recording started from the popup runs (Go core)

Owner 2026-11-08 (answer 12). ADR-0032 «Addendum 2026-11-08». The UI side is TASK-406.

## Done
- `recordingprompt.Service.Start` remembers the prompt it started a recording from (prompt id, lesson, recording id) under `s.mu`; only a successful popup start writes it, `StartManual` does not. Newest start overwrites.
- `CallSubject(id)` answers for the current OFFERED lesson prompt as before, and now also for the remembered started prompt while its recording runs. After stop, other id, ad-hoc (empty lesson), main-window start, port not wired: "". Lesson -> subjectmatch -> link code and the one-code/no-cause log are shared by both paths.
- New optional port `RunningRecordings.IsRunning(recordingID)` in `ports.go` (`Deps.Running`, nil-safe; the existing `Recordings` interface is untouched so `desktop/tests` fakes did not change). Wired in `app/prompts.go`: `session.Active()` with the same ID and status STARTING or RECORDING. Called outside `s.mu`.
- Tests: `calllink_running_test.go` (table on fakes: offered, running, after stop, foreign id, ad-hoc, later ad-hoc, main-window start, failed start, port nil, link/lesson removed, newer prompt on offer, race with Start/OfferAdHoc, log has no secrets) and an app-level test on the real core (`TestPromptCallSubjectWhileItsRecordingRuns`).

## Decisions to confirm
- «Running» = status STARTING/RECORDING; a recording being stopped is not running.
- The started-prompt entry is never cleared (recording IDs are unique, so a stale entry is harmless).

## Follow-ups
- Stop-requested window: between the stop request and the session publishing STOPPING, `Active()` still says RECORDING, so «Подключиться» may answer once more for a moment. Closing it needs `recordingsession` to expose «stop requested» (like `Sound` does); out of this task's files.
- TASK-406 (UI): the popup must keep asking `CallSubject` with the id of the prompt it started the recording from; the core answers for it although it is no longer current.
- Optional: a one-line comment that the `startedPrompt` entry may outlive its recording on purpose; a small `snapshot()` helper in `promptLesson` for the unlock paths (reviewer nits).

## Changed existing tests
`calllink_test.go`: `callEnv` gains `running *fakeRunning`, passed as `Deps.Running` (no assertion changed; nothing runs by default, so the TASK-370 cases behave as before).

## Review
Reviewer (opus): approve, three lows and two nits (above). security-auditor (opus): no findings.
Security: the page still receives only a subject id; no link, host or cause in logs; the new branch matches the exact in-process prompt id and requires a running recording.
