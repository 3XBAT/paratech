# ADR-0009: Desktop core wiring and lifecycle

- **Status:** Accepted
- **Source:** TASK-056, extended by TASK-022, TASK-049, TASK-057, TASK-060, TASK-030, TASK-065, TASK-058, TASK-051; builds on ADR-0007 (Wails shell)

## Context
The use cases existed, but nothing built them: no job ran, no recording recovered, the page's Stop / Run now were stubs.
Constraints from earlier tasks: wiring only in `internal/app` (ARCH-50); recovery must run while no other process can own
a RUNNING job or an unfinished recording; Wails' single-instance check runs inside `Shell.Run`, and the shell is not ready
(no windows) until `ApplicationStarted`.

## Decision
- **Two phases.** `openCore` (sync, no goroutines): data dir -> instance lock -> SQLite (migrate) -> Storage Manager
  (`filestore`) -> use cases and job registrations; returns `wails.Core{Settings, Activity, Stopper, Forcer, Prompt,
  Schedule, Files, Models, Downloads, Imports, Links, Speech, Connections, Google, Calendar}` (interfaces required;
  `Prompt` since TASK-022, `Schedule` and `Files` since TASK-049, the model fields since TASK-030, `Connections` since
  TASK-062, `Google` since TASK-050, `Calendar` since TASK-051).
  `start` runs
  once: UI mode from `Options.Started` (Wails' `ApplicationStarted`) in an app goroutine; headless right after
  `openCore` inside `Run` (headless now opens the core; no data dir is an error).
- **Single instance.** UI: Wails' named mutex first (a second UI instance hands off to the window and exits before
  `OpenCore`). Then, for every mode, a file lock `<DataDir>/learning-assistant.lock` taken before the database:
  `LockFileEx` exclusive, fail-immediately on Windows; `flock` on Unix; other systems `ErrUnsupported`. The file is never
  deleted; the OS drops the lock when the process dies. A held lock -> `openCore` fails with `instancelock.ErrHeld`.
- **Start order:** startup cleanups (filestore temp files, whisper-cli work dirs; errors only logged) ->
  `jobrunner.Recover` -> `recordingsession.Recover` in the background (long assemblies must not delay jobs) ->
  `senddelivery.Recover` -> `senddelivery.Resume` -> transcription sweep (`transcribe.Enqueuer.EnqueuePending`, TASK-060;
  errors only logged) -> calendar sync request (when signed in, TASK-051; only logs) -> `runner.Start` -> loops
  (UI mode: `scheduler.Run`, `recordingprompt.Run`). No binding may
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
| `stt` | yes | requeue | 6 h | CPU-only, no side effects; long recordings on slow laptops. Registered only when the speech component verified (at start or after the in-app install) |
| `summarize`, `extract_homework` | no | requeue | 10 min (5 x hub request timeout) | wait on the Hub; idempotent by key |
| `homework_draft` | no | requeue | 10 min | same |
| `send_email` | no | reconcile, uncancellable | 2 x SMTP timeout + 1 min | external side effect (hard rule 7) |
| `calendar_sync` | no | requeue | 30 min | external, but idempotent upserts by stable event ids (TASK-051, ADR-0011) |

Use-case `MaxAttempts` = the runner's backoff `MaxAttempts`. The STT completion event goes to the post-processor, which
enqueues `summarize`/`extract_homework` with key `<type>:<transcript id>:v<prompts version>`.

- **No completion notifications (TASK-065, owner decision 2026-10-15).** The only notification is the lesson-started
  popup; the user follows processing on the «Записи и задачи» page. The TASK-053 path is removed, not kept unused:
  `cases/notify`, `domain/jobs/notice`, `notifypolicy`, the job runner `Observer`, the Wails toasts and the setting
  toggle. Migration 0008 drops `job_notices` and `settings.notify_on_processing_complete` (applied migrations stay
  unchanged). The shell registers no Wails notifications service, so it writes no notification keys under HKCU; keys
  left by older builds are harmless. `tests/no_completion_toasts_test.go` fails if the notifications package or
  go-toast comes back; `TestFinishedJobsShowNothing` (app) checks that finished jobs show nothing.

- **Recording -> STT (TASK-060).** `recordingsession` calls its `CompletionListener` after a recording is saved COMPLETED
  with a track (stop, hard cap, capture ended, startup recovery); the app passes `transcribe.Enqueuer`. In one `Tx.Do` it
  pins the model (`modelcatalog.ResolveForJob` through a bridge) and enqueues key `stt:<recording id>:<model id>`. A
  recording that has any STT job gets none (no second job when the default model changed). No model: no job, a WARN
  line. A crash between the save and the call loses the call; the startup sweep (COMPLETED, track, no transcript)
  catches up, so delivery is at least once.
- **No whisper-cli (TASK-060, TASK-072).** The `stt` type is registered only with a verified speech component
  (`cases/speechcomponent`, folders and manifest in docs/refs/whisper.md). Otherwise STT jobs stay QUEUED (the runner
  claims and recovers only registered types). The in-app install registers the type at run time with
  `jobrunner.Service.Activate` (it first recovers stale RUNNING jobs of the type, then rebuilds the claim classes and
  wakes the workers), so the same job runs without a restart. The permanent STT stand-in of TASK-056 is gone.
- **Heavy cap (TASK-060).** `jobrunner.Config.MaxHeavy` (default 1): a worker reserves a heavy slot before the claim and
  frees it after the attempt; without a free slot it claims only light types. So 2 workers never run 2 whisper-cli.
- **Models (TASK-060, TASK-030).** `modelcatalog.Service`, `Downloader` (`httpdownload`, `filestore` free space) and
  `Importer` (`localfile`) sit behind `app.modelManager`, which the models page binds to. It owns one background
  download at a time (cancelled at shutdown, partial bytes kept), lets writes start only after the startup cleanups,
  and after every install and every default change runs `EnqueuePending` in the background (passes never overlap;
  errors only WARN), so waiting recordings get their STT job without a restart. The first model needs no default
  (`Resolve` takes the only installed one); with several and no default the jobs wait for SetDefault. The speech component
  status is read live (`wails.Core.Speech`). Shutdown stops the manager before the storage (a closer, 15 s).
  Source pages open through `adapters/weblink`: `%WINDIR%\explorer.exe <url>` after a strict link check, no shell.
  The catalog is an embedded manifest with `"models": []` (`internal/app/catalog.json`; the domain accepts an explicit
  empty list): no ggml model has a verified SHA-256 yet, so the catalog list is empty and download has nothing to
  offer. Manual import, the default and the job pin work.

- **Hub, mail, drafts (TASK-057).** `hubclient` and `smtp` read the Hub URL and the mail account from the settings at
  every call (bridges in `internal/app/settingsbridge.go`, mapping only); the API key and the SMTP password come from
  one `wincred` store per service (`hub`, `mail`), shared by the adapter (reads) and `senddelivery` (writes). Not
  configured: LLM jobs park in WAITING_FOR_NETWORK ("hub: not configured", no attempts used), an email is
  `delivery.ErrRejected`. Drafts: `sqlite.DraftRepository`. Off Windows the stores fail with `ErrUnsupported`.
- **Action proposals (TASK-060 part 2).** `homeworkflow.Service` (`app/proposals.go`): the model proposes, the core
  validates, the user confirms, then the port acts. Proposals: `sqlite.ProposalRepository` (table `proposals`, version
  CAS; no foreign key to assignments, so an unknown assignment is stored REJECTED and a deleted one makes a confirmed
  proposal STALE). Mailer: `senddelivery.Service` (one delivery per command key, an UNKNOWN one is never resent).
  Exporter: `filestore.Store.ExportHomework` (only `exports/<assignment>/homework.md`). Recipient: `recipientSettings`.
  Calendar: `googlecal` through `deadlineCalendar` (TASK-051); not signed in = `homeworkflow.ErrCalendarNotConnected`,
  the proposal stays CONFIRMED.
  Clock `e.local`, like the other `homeworkflow` services.
- **Homework page (TASK-045).** `wails.Core.Homework` = proposals, drafts and deliveries (`input/wails.HomeworkService`).
  `PrepareSend` = `homeworkflow.Service.ProposeSend` (the user's send goes through `Propose`: VALIDATED with recipient
  and attachment frozen, or REJECTED); `ConfirmSend` = `homeworkflow.Service.ConfirmSend` (`Confirm` then `Execute`;
  safe to repeat: same command key), the only binding that sends. `ExportDraft` is refused while an email of the
  assignment is unresolved (`senddelivery.Service.HasUnresolved`): the file is its attachment. Lists:
  `sqlite.AssignmentRepository.ListRecent` (200) and `sqlite.DeliveryRepository.ListByAssignment` (50).
  `tests/userconfirmation_test.go`: the names `Confirm`, `Execute`, `ConfirmSend`, `MarkSent`, `SendAgain` appear in
  production code only in `input/wails/homework.go` and in the use case files that implement them
  (`homeworkflow/proposals.go`, `senddelivery/service.go`, `senddelivery/job.go`).
- **Connections (TASK-062).** `cases/connections` (`app/connections.go`) edits the mail account and the Hub URL in the
  settings and writes the SMTP password into the same `mail` store (`smtp-password`; set/exists/delete only, never
  read back). "Check mail" = `smtp.Mailer.Check`: Send's session setup (EHLO, STARTTLS, AUTH), then QUIT, no message;
  the bridge maps the adapter's session classes to the use case's results. Off Windows the page shows "unsupported".
  TASK-039: the Hub key is the second slot (`hub` store, `api-key`, checked by `values.ParseHubAPIKey`). "Refresh"
  reads `hubclient.GetRemaining` on the core's client (bridge `hubQuota`); the last snapshot lives in memory only:
  stale after a failure, dropped for a missing address or key or a rejected key, forgotten when either changes.
- **Recorder (TASK-017).** Windows: `audiocapture.Recorder`, which runs this executable as the recorder child
  (ADR-0002). Other OSes: `audiocapture.Unavailable`, every call fails; its `Finalize` is not `ErrNoAudio`, so
  interrupted recordings keep their chunks for a later recovery.
- **Recorder run mode** (`--recorder --data-dir`): no instance lock (the parent holds it), no database, no UI shell,
  no log file, no jobs; only the Storage Manager on `<DataDir>/artifacts`. Events on stdout, log on stderr, stops when
  stdin closes. A self-test request (TASK-058) plays the tone of `audiocapture.PlatformTone` and writes nothing.
- **Log file (TASK-058).** `app.New` wraps the logger of `main` in `logfile.Tee` (main.go unchanged); everything logs
  through it, the Wails logger behind its Info floor too. `openCore` attaches `<DataDir>/logs/desktop.log` (5 MiB,
  `desktop.1.log`, `desktop.2.log`) right after the instance lock, so a second instance never writes it; a file that
  cannot be opened is a Warn on stderr. `Close` detaches and closes it after the storage.
- **Diagnostics page (TASK-058).** `input/wails.DiagnosticsService`: `CheckSound` (`cases/soundcheck`: the recorder's
  `CheckSound`, refused while a recording runs, verdict for the remembered source) and `SaveSupportBundle`
  (`cases/supportbundle`: native save dialog, then `zipfile` writes about.json, settings.json, capture.json and
  `logs/` with the profile directory as `%USERPROFILE%`; `sysinfo` reads the versions). Nothing is uploaded.
- **Clocks.** `clock.System` (UTC, with `Sleep`) for every use case; `clock.Local` only for `homeworkflow` (the
  post-processing, the drafts and the proposals), which places lesson times and deadlines in the student's zone.
  SQLite stores Unix ms, so the zone does not affect storage.
- **IDs.** `random.IDs`: lowercase `crypto/rand.Text()`; Message-ID `<id@learning-assistant.invalid>`. Jitter via `crypto/rand`.
- **Data dir:** `learning-assistant.db`, `learning-assistant.lock`, `artifacts/` (Storage Manager root), `artifacts/stt-work/`,
  `logs/`.

## Not wired yet (follow-ups)
- Catalog entries with verified SHA-256 in `internal/app/catalog.json`.
- The SHA-256 pin check of the whisper-cli binary (docs/refs/whisper.md), TASK-054 or later.
- Wired since: the calendar sync of `schedulesync` and the deadline calendar (`googlecal`) by TASK-051 (a sync is also
  requested at start, after the transcription sweep, when signed in); `scheduler` by TASK-022 (UI mode only); the `schedulesync` import for the schedule page by TASK-049;
  the Windows recorder by TASK-017; recording -> STT, `modelcatalog`, the heavy cap and `homeworkflow.Service` by
  TASK-060; the models page by TASK-030 (hardware detection for the recommendation is not wired: the catalog is listed
  for an unknown machine); the Hub key and the quota of the «Подключения» page by TASK-039; the bindings of the
  drafts, the proposals and the deliveries (homework page) by TASK-045.
- The calendar sentinel lives next to its port in `homeworkflow`, so `input/wails` can match it with `errors.Is`
  without importing `app` (ARCH-40).

## Consequences
- ARCH-50 holds: only `internal/app` imports cases and adapters together; the new adapters (`random`, `instancelock`,
  `audiocapture`, `weblink`) import only domain and std (`instancelock`, `audiocapture` and `weblink` also
  `x/sys/windows`).
- `userfile` (TASK-049, std only) reads the file chosen for the schedule import, so the binding does no file access.
- TASK-058 adapters: `logfile`, `zipfile` (std only), `sysinfo` (std, `x/sys/windows` and its `registry`, read only).
- The page's "not available yet" error is gone; Stop / Run now always reach the use cases.
- Open owner decisions: STT timeout 6 h; model alias `default`; Message-ID domain `learning-assistant.invalid`; LLM jobs
  show "waiting for network" while the Hub is not configured; UI mode shows no error dialog when the start fails.
