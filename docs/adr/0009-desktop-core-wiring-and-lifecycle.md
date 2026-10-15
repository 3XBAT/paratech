# ADR-0009: Desktop core wiring and lifecycle

- **Status:** Accepted
- **Source:** TASK-056; builds on ADR-0007 (Wails shell)

## Context
The use cases existed, but nothing built them: no job ran, no recording recovered, the page's Stop / Run now were stubs.
Constraints from earlier tasks: wiring only in `internal/app` (ARCH-50); recovery must run while no other process can own
a RUNNING job or an unfinished recording; Wails' single-instance check runs inside `Shell.Run`, and the shell is not ready
(no toasts) until `ApplicationStarted`.

## Decision
- **Two phases.** `openCore` (sync, no goroutines): data dir -> instance lock -> SQLite (migrate) -> Storage Manager
  (`filestore`) -> use cases and job registrations; returns `wails.Core{Settings, Activity, Stopper, Forcer, Prompt,
  Schedule, Files}` (all required; `Prompt` since TASK-022, `Schedule` and `Files` since TASK-049). `start` runs
  once: UI mode from `Options.Started` (Wails' `ApplicationStarted`) in an app goroutine; headless right after
  `openCore` inside `Run` (headless now opens the core; no data dir is an error).
- **Single instance.** UI: Wails' named mutex first (a second UI instance hands off to the window and exits before
  `OpenCore`). Then, for every mode, a file lock `<DataDir>/learning-assistant.lock` taken before the database:
  `LockFileEx` exclusive, fail-immediately on Windows; `flock` on Unix; other systems `ErrUnsupported`. The file is never
  deleted; the OS drops the lock when the process dies. A held lock -> `openCore` fails with `instancelock.ErrHeld`.
- **Start order:** startup cleanups (filestore temp files, whisper-cli work dirs; errors only logged) ->
  `jobrunner.Recover` -> `recordingsession.Recover` in the background (long assemblies must not delay jobs) ->
  `senddelivery.Recover` -> `senddelivery.Resume` -> `runner.Start` -> loops (UI mode: `scheduler.Run`,
  `recordingprompt.Run`). No binding may write artifacts or start a recording before `start` has run the cleanups and
  the recording recovery has marked interrupted recordings (TASK-022 binding). TASK-022 closes it with
  `recordingsession.RecoveryMarked()`: closed when the first `Recover` ends its marking pass (or fails before it), or
  by `recordings.Close` (a failed start: Start then fails, not "recovering" forever); the prompt refuses Start until
  then ("recordings are being recovered, try again shortly").
- **Stop order** (`Close`, idempotent, safe without start): cancel and wait for the startup -> cancel and wait for the
  loops (15 s; a window command may hang once the shell is gone) -> `runner.Stop` (15 s) ->
  `recordings.Close` (15 s) -> wait for the background recovery -> filestore, database -> instance lock.
- **Failures.** `openCore` unwinds what it opened. A failed `jobrunner.Recover` or `runner.Start` stops the started work;
  the other steps only log. Headless returns the error; UI mode logs it and keeps the window (settings, lists).

| Job type | Heavy | Recovery | Timeout | Why |
|---|---|---|---|---|
| `stt` | yes | requeue | 6 h | CPU-only, no side effects; long recordings on slow laptops |
| `summarize`, `extract_homework` | no | requeue | 10 min (5 x hub request timeout) | wait on the Hub; idempotent by key |
| `homework_draft` | no | requeue | 10 min | same |
| `send_email` | no | reconcile, uncancellable | 2 x SMTP timeout + 1 min | external side effect (hard rule 7) |

Use-case `MaxAttempts` = the runner's backoff `MaxAttempts`. The STT completion event goes to the post-processor, which
enqueues `summarize`/`extract_homework` with key `<type>:<transcript id>:v<prompts version>`. Notifications cover the four
non-email types, UI mode only (headless has no notifier).

- **Hub, mail, drafts (TASK-057).** `hubclient` and `smtp` read the Hub URL and the mail account from the settings at
  every call (bridges in `internal/app/settingsbridge.go`, mapping only); the API key and the SMTP password come from
  one `wincred` store per service (`hub`, `mail`), shared by the adapter (reads) and `senddelivery` (writes). Not
  configured: LLM jobs park in WAITING_FOR_NETWORK ("hub: not configured", no attempts used), an email is
  `delivery.ErrRejected`. Drafts: `sqlite.DraftRepository`. Off Windows the stores fail with `ErrUnsupported`.
- **Stand-ins** (`internal/app/standins.go`): STT engine -> permanent, used when `whisper-cli(.exe)` next to the
  executable is missing or refused (logged at startup).
- **Recorder (TASK-017).** Windows: `audiocapture.Recorder`, which runs this executable as the recorder child
  (ADR-0002). Other OSes: `audiocapture.Unavailable`, every call fails; its `Finalize` is not `ErrNoAudio`, so
  interrupted recordings keep their chunks for a later recovery.
- **Recorder run mode** (`--recorder --data-dir`): no instance lock (the parent holds it), no database, no UI shell,
  no jobs; only the Storage Manager on `<DataDir>/artifacts`. Events on stdout, log on stderr, stops when stdin closes.
- **Clocks.** `clock.System` (UTC, with `Sleep`) for every use case; `clock.Local` only for `homeworkflow`, which places
  lesson times and deadlines in the student's zone. SQLite stores Unix ms, so the zone does not affect storage.
- **IDs.** `random.IDs`: lowercase `crypto/rand.Text()`; Message-ID `<id@learning-assistant.invalid>`. Jitter via `crypto/rand`.
- **Data dir:** `learning-assistant.db`, `learning-assistant.lock`, `artifacts/` (Storage Manager root), `artifacts/stt-work/`.

## Not wired yet (follow-ups)
- Enqueueing STT when a recording completes (model pin, key, startup sweep): no STT job is created in production yet.
  The STT stand-in fails jobs permanently, so a job keyed `stt:<recording>:<model>` that failed while whisper-cli was
  missing would block transcription after the install is fixed: that task must handle it (e.g. a parking class or a new key round).
- `modelcatalog`, `homeworkflow.Service`, proposals and the calendar sync of `schedulesync`.
- Wired since: `scheduler` by TASK-022 (UI mode only); the `schedulesync` import for the schedule page by TASK-049; the
  Windows recorder by TASK-017.
- Settings UI for mail and Hub, bindings of the drafts (TASK-045) and of the Hub key (TASK-039).

## Consequences
- ARCH-50 holds: only `internal/app` imports cases and adapters together; the new adapters (`random`, `instancelock`,
  `audiocapture`) import only domain and std (`instancelock` and `audiocapture` also `x/sys/windows`).
- `userfile` (TASK-049, std only) reads the file chosen for the schedule import, so the binding does no file access.
- The page's "not available yet" error is gone; Stop / Run now always reach the use cases.
- Open owner decisions: STT timeout 6 h; model alias `default`; Message-ID domain `learning-assistant.invalid`; LLM jobs
  show "waiting for network" while the Hub is not configured; UI mode shows no error dialog when the start fails.
