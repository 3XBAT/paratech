# ADR-0009: Desktop core wiring and lifecycle

- **Status:** Accepted
- **Source:** TASK-056, extended by TASK-022, TASK-049, TASK-057, TASK-060; builds on ADR-0007 (Wails shell)

## Context
The use cases existed, but nothing built them: no job ran, no recording recovered, the page's Stop / Run now were stubs.
Constraints from earlier tasks: wiring only in `internal/app` (ARCH-50); recovery must run while no other process can own
a RUNNING job or an unfinished recording; Wails' single-instance check runs inside `Shell.Run`, and the shell is not ready
(no toasts) until `ApplicationStarted`.

## Decision
- **Two phases.** `openCore` (sync, no goroutines): data dir -> instance lock -> SQLite (migrate) -> Storage Manager
  (`filestore`) -> use cases and job registrations; returns `wails.Core{Settings, Activity, Stopper, Forcer, Prompt,
  Schedule, Files, Connections}` (all required; `Prompt` since TASK-022, `Schedule` and `Files` since TASK-049,
  `Connections` since TASK-062). `start` runs
  once: UI mode from `Options.Started` (Wails' `ApplicationStarted`) in an app goroutine; headless right after
  `openCore` inside `Run` (headless now opens the core; no data dir is an error).
- **Single instance.** UI: Wails' named mutex first (a second UI instance hands off to the window and exits before
  `OpenCore`). Then, for every mode, a file lock `<DataDir>/learning-assistant.lock` taken before the database:
  `LockFileEx` exclusive, fail-immediately on Windows; `flock` on Unix; other systems `ErrUnsupported`. The file is never
  deleted; the OS drops the lock when the process dies. A held lock -> `openCore` fails with `instancelock.ErrHeld`.
- **Start order:** startup cleanups (filestore temp files, whisper-cli work dirs; errors only logged) ->
  `jobrunner.Recover` -> `recordingsession.Recover` in the background (long assemblies must not delay jobs) ->
  `senddelivery.Recover` -> `senddelivery.Resume` -> transcription sweep (`transcribe.Enqueuer.EnqueuePending`, TASK-060;
  errors only logged) -> `runner.Start` -> loops (UI mode: `scheduler.Run`, `recordingprompt.Run`). No binding may
  write artifacts or start a recording before `start` has run the cleanups and the recording recovery has marked
  interrupted recordings (TASK-022 binding). TASK-022 closes it with
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
| `stt` | yes | requeue | 6 h | CPU-only, no side effects; long recordings on slow laptops. Registered only when whisper-cli is usable |
| `summarize`, `extract_homework` | no | requeue | 10 min (5 x hub request timeout) | wait on the Hub; idempotent by key |
| `homework_draft` | no | requeue | 10 min | same |
| `send_email` | no | reconcile, uncancellable | 2 x SMTP timeout + 1 min | external side effect (hard rule 7) |

Use-case `MaxAttempts` = the runner's backoff `MaxAttempts`. The STT completion event goes to the post-processor, which
enqueues `summarize`/`extract_homework` with key `<type>:<transcript id>:v<prompts version>`. Notifications cover the four
non-email types, UI mode only (headless has no notifier).

- **Recording -> STT (TASK-060).** `recordingsession` calls its `CompletionListener` after a recording is saved COMPLETED
  with a track (stop, hard cap, capture ended, startup recovery); the app passes `transcribe.Enqueuer`. In one `Tx.Do` it
  pins the model (`modelcatalog.ResolveForJob` through a bridge) and enqueues key `stt:<recording id>:<model id>`. A
  recording that has any STT job gets none (no second job when the default model changed). No model: no job, a WARN
  line. A crash between the save and the call loses the call; the startup sweep (COMPLETED, track, no transcript)
  catches up, so delivery is at least once.
- **No whisper-cli (TASK-060).** The `stt` type is registered only with a usable engine. Otherwise STT jobs stay QUEUED
  (the runner claims and recovers only registered types), and the same job runs after the install and a restart. The
  permanent STT stand-in of TASK-056 is gone.
- **Heavy cap (TASK-060).** `jobrunner.Config.MaxHeavy` (default 1): a worker reserves a heavy slot before the claim and
  frees it after the attempt; without a free slot it claims only light types. So 2 workers never run 2 whisper-cli.
- **Models (TASK-060).** `modelcatalog.Service`, `Downloader` (`httpdownload`, `filestore` free space) and `Importer`
  (`localfile`) are on the core for TASK-030 (no bindings yet). The catalog is an embedded manifest with `"models": []`
  (`internal/app/catalog.json`; the domain accepts an explicit empty list): no ggml model has a verified SHA-256 yet,
  so the catalog list is empty and download has nothing to offer. Manual import, the default and the job pin work.

- **Hub, mail, drafts (TASK-057).** `hubclient` and `smtp` read the Hub URL and the mail account from the settings at
  every call (bridges in `internal/app/settingsbridge.go`, mapping only); the API key and the SMTP password come from
  one `wincred` store per service (`hub`, `mail`), shared by the adapter (reads) and `senddelivery` (writes). Not
  configured: LLM jobs park in WAITING_FOR_NETWORK ("hub: not configured", no attempts used), an email is
  `delivery.ErrRejected`. Drafts: `sqlite.DraftRepository`. Off Windows the stores fail with `ErrUnsupported`.
- **Connections (TASK-062).** `cases/connections` (`app/connections.go`) edits the mail account and the Hub URL in the
  settings and writes the SMTP password into the same `mail` store (`smtp-password`; set/exists/delete only, never
  read back). "Check mail" = `smtp.Mailer.Check`: Send's session setup (EHLO, STARTTLS, AUTH), then QUIT, no message;
  the bridge maps the adapter's session classes to the use case's results. Off Windows the page shows "unsupported".
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
- `homeworkflow.Service` and proposals (TASK-060 part 2); the calendar sync of `schedulesync` (TASK-051).
- Catalog entries with verified SHA-256 in `internal/app/catalog.json`.
- The SHA-256 pin check of the whisper-cli binary (docs/refs/whisper.md), TASK-054 or later.
- A model installed while the app runs gets its waiting recordings enqueued only at the next start (TASK-030 can call
  the sweep after an install).
- Wired since: `scheduler` by TASK-022 (UI mode only); the `schedulesync` import for the schedule page by TASK-049;
  the Windows recorder by TASK-017; recording -> STT, `modelcatalog` and the heavy cap by TASK-060.
- Bindings of the drafts (TASK-045) and of the Hub key (TASK-039; a second secret slot in `cases/connections`).

## Consequences
- ARCH-50 holds: only `internal/app` imports cases and adapters together; the new adapters (`random`, `instancelock`,
  `audiocapture`) import only domain and std (`instancelock` and `audiocapture` also `x/sys/windows`).
- `userfile` (TASK-049, std only) reads the file chosen for the schedule import, so the binding does no file access.
- The page's "not available yet" error is gone; Stop / Run now always reach the use cases.
- Open owner decisions: STT timeout 6 h; model alias `default`; Message-ID domain `learning-assistant.invalid`; LLM jobs
  show "waiting for network" while the Hub is not configured; UI mode shows no error dialog when the start fails.
