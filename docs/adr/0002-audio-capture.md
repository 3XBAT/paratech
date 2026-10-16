# ADR-0002 (audio capture): system audio capture on Windows

- **Status:** Accepted (spike on Windows 10 19045 without real sound; real-signal checks in TASK-016, see Verification)
- **Source:** TASK-015 (spike S-002), PRD §4.3 (audio row), §6.2, §6.3, AC-03, AC-05, AC-24; API notes in `docs/refs/audio.md`;
  code in `paratech/desktop/spikes/audio-capture/`
- **Numbering:** TASK-015/016/017 and ADR-0001 call this "ADR 0002". It is unrelated to `0002-initial-bounded-contexts.md`;
  cite it as "ADR-0002 (audio capture)".

## Context
PRD §4.3 leaves the capture method to a spike: malgo (CGO), pure-Go WASAPI, or a recorder process. Hard rule 1 forbids CGO in
the desktop binary. The owner's machine is Windows 10 22H2 (19045); process loopback is documented only from build 20348.

## Decision
1. **Source = the lecture app the user picks** (owner decision) from a menu of open apps (browser, Zoom, Teams, Telemost...):
   process loopback `PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE` on the tree root of that app. "Whole computer sound" is
   only a fallback menu entry: process loopback EXCLUDE of our own PID, then classic loopback.
2. **Own pure-Go WASAPI code:** `golang.org/x/sys/windows` + `syscall`, COM through vtables, no new dependency, **no CGO**
   (hard rule 1 holds; the CGO exception foreseen in ADR-0001 is not used). malgo is rejected: no process loopback, needs CGO.
3. **Automatic fallback chain**, one-way within a recording; every switch is logged and stored as a problem of the recording:
   - process loopback INCLUDE(chosen app) → classic loopback on the default render endpoint (eRender/eConsole, shared, mix
     format) with a silent keep-alive render stream, reopened on default-device change or `AUDCLNT_E_DEVICE_INVALIDATED` with
     bounded backoff.
   - Switch when activation fails or times out (5 s), or the stream is **healthy but mute**: only zeros for longer than a
     threshold while audio is expected (the chosen app's audio session is Active with a non-zero peak meter; whether the
     meter reads before or after session volume is not confirmed). Threshold:
     proposal 15 s, tuned in TASK-016/058. Reason: MeetCap #38 (zeros on Windows 11 26200).
   - EXCLUDE is not a step of this chain: when INCLUDE fails, process loopback as a whole most likely fails on that build.
4. **Recorder child process** (owner decision): the product recorder runs in a child process of the same binary (flag
   `--recorder`) for crash isolation (in-process would lose it), stopped by closing its stdin and killed after a grace period
   (spike: 10 s). It writes closed WAV chunks of 30-60 s. The `Recorder` port (`internal/cases/recordingsession/ports.go`:
   Start/Stop/Wait/Finalize) stays the same; the child is an adapter detail. TASK-059 adds the source (chosen app) to `Start`.
5. **Chunk format:** `chunk-NNNNNN.wav.partial` with a placeholder header (RIFF size 0); at the end the final header is
   patched, the file fsynced, closed and renamed to `chunk-NNNNNN.wav`. A final name is always a complete WAV. A kill loses
   the current chunk (up to one chunk length), left as `.partial` (reported by validation, skipped by `Finalize`); up to
   64 KiB (~170 ms at 48 kHz float32 stereo) of it may still be in the user-space buffer. Rotation is counted in frames; a format change closes the chunk; numbering continues
   after the highest existing chunk, so a restart never overwrites. Windows cannot fsync a directory.
6. **Format and timeline:** process loopback has no mix format (`GetMixFormat` = `E_NOTIMPL`): request 48 kHz float32 stereo
   (accepted on 19045), fallback 44.1 kHz s16 stereo. Classic loopback uses the mix format. Duration = frames / rate, never
   wall clock; gaps (reopen, mode switch) are filled with silence measured by the monotonic clock.
7. **Spike code location:** `paratech/desktop/spikes/` is a new top-level directory of the desktop module, allowed by this
   ADR under ARCH-01 for throwaway prototypes only. `tests/arch_test.go` allows it and fails on any import of it from outside
   `spikes/`; it is not part of the product binary. The malgo prototype is a separate nested module
   (`spikes/audio-capture/malgo`, own `go.mod`, not in `go.work`, not built by CI), so the desktop module has no CGO
   dependency. Decided with TASK-017: keep `spikes/audio-capture/` until TASK-016 is done (the owner runs `capturespike`
   there), then delete it.

## Candidates (spike on Windows 10 19045, nothing played)
| | A: malgo | B: pure Go, process loopback | B': pure Go, classic loopback | C: C++/Rust helper | C': same Go binary as child |
|---|---|---|---|---|---|
| Prototype | `spikes/.../malgo` | `wasapi/`, `-mode include/exclude` | `wasapi/`, `-mode system` | not built | `capturespike -child` / `--recorder` |
| CGO (rule 1) | **yes**, violates | no | no | not in our binary | no |
| Process loopback | no (not in malgo/miniaudio) | yes | - | yes (MS sample, wasapi-rs) | as B |
| Reliability | 0 callbacks in silence (no keep-alive); miniaudio had rerouting crash fixes | activated first try in INCLUDE and EXCLUDE; unofficial below 20348; own unsafe COM code | needs keep-alive; Win10 BT HFP gap | ready MIT code, small projects | isolates crashes of unsafe/COM code |
| Device change | miniaudio auto rerouting, for loopback not confirmed; not tested | endpoint-independent; not tested | poll 2 s + reopen, backoff 250 ms..4 s, 5 tries; not tested with a real change | own code | as B/B' |
| Chunks after kill | same writer pattern; kill not tested; always starts at chunk 1: a rerun into the same dir overwrites earlier chunks (or fails on O_EXCL if a `.partial` was left) | valid (tested via C') | valid (tested via C') | own code | chunks 1-2 valid, 3 left `.partial`; restart continues at 4 |
| CPU, 10 min | not measured | 3.6 s (~0.6 % of a core) | 10.3 s (~1.7 %, incl. keep-alive) | - | child = B (run was in `-child`) |
| Build | `CGO_ENABLED=1` + MSYS2 mingw gcc 14.2.0, ~6 MB exe | plain `go build`, `CGO_ENABLED=0` | same | second toolchain, second exe to sign (SmartScreen, AV) | same as B |

**Choice:** B with B' as fallback, run as C'. Only option without CGO that gives both APIs; C' gives C's isolation without a second
language or a second exe.

## Measured facts (19045, RtlGetVersion)
| Run | Result |
|---|---|
| system, 6 s | mix format 48 kHz 2 ch float32 (EXTENSIBLE); 600 packets (one per 10 ms); 3 valid chunks, all zeros |
| exclude own PID / include `explorer.exe` (root PID 7532) | activation ok, 48 kHz float32 stereo accepted; 600 packets; 3 valid chunks |
| include `-pid 3` (no such process) | **activation succeeds**, then silence |
| auto `-exe nosuchapp.exe` | target lookup fails → system ok |
| 10 min include in `-child` | 28,781,760 frames = 599.62 s vs 600.0 s wall (0.38 s short, cause unknown); 20/20 chunks valid |
| 10 min system | 28,800,000 frames = 600.000 s; 20/20 valid |
| silence | process loopback delivers packets continuously without any render stream; classic needs the keep-alive |
| startup | stream running ~40-50 ms after process start (process loopback), ~230-460 ms (system) |
| `taskkill /F` of the child | parent sees exit 1; chunks 1-2 valid; `chunk-000003.wav.partial` invalid ("RIFF size 0 does not match file size 524288") |

Not tested: real signal (include/exclude filtering), device change and reopen, a real activation failure, parent-initiated
stop end to end, Bluetooth/HFP, sleep/resume, privacy toggle, 3 h, Defender/SmartScreen, Windows 11.

## Known limits (tell the user)
- A **muted browser tab** or muted app (speaker off in Zoom/Teams/Telemost), or the app's **mixer slider** at 0/muted, gives no
  audio: Chromium closes the system stream on tab mute; session volume applies before the tap (forum reports, not confirmed by MS).
- Lowering the **Windows master volume** or the headphones should be fine for INCLUDE (needs a manual check per device).
- All tabs of a browser are one process tree: another video in the **same browser** is recorded. Per-tab isolation needs a
  browser extension (not in MVP). A video in another app or another browser is not recorded.
- Ducking during a call may lower the lecture. Bluetooth HFP on Windows 10 matters only for the classic fallback.
  DRM content is not delivered.
- If the chosen app is closed and its recorder child ends while waiting for it, a gap longer than the lead-silence cap
  (~154 s) shortens the track timeline (logged as Warn).
- User advice text: "To stop hearing the lecture, turn down the Windows or headphone volume. Do not mute the tab, the app or
  its slider in the volume mixer."

## Consequences
- **PRD:** §6.3 says "everything the computer plays"; this ADR narrows it to the chosen app (whole computer only as fallback).
  **Open (owner):** update PRD §6.3 and AC-24.
- **tasks.json:** TASK-058 names "process loopback EXCLUDE" as the main mode; superseded by this ADR (INCLUDE of the chosen app).
  The task text is not edited here.
- **TASK-017 (Recorder adapter):** pure Go per Decision 2-6; may copy and harden spike code, never import `spikes/`. Explicit
  format; frame-based timeline and silence for gaps; `Finalize` accepts chunks of different formats (a mode switch changes
  the format). `Wait` returns an error when capture ends by itself (reopen budget exhausted, child died). Also:
  - Recorder child per Decision 4; TASK-017 decides only how the `--recorder` run mode is wired (cmd -> config/app per ARCH-51;
    an ADR if it does not fit).
  - Drain the endpoint buffer once on Stop (after `IAudioClient::Stop`), so the last packets reach the chunk.
  - Each process loopback activation keeps a handler, its parameters and an event alive for the process lifetime: bound
    activation retries per recording or reuse one handler and event per stream.
  - Keep recordings under `%LOCALAPPDATA%` (user-only ACL) or set an explicit DACL; Go mode bits (0700/0600) set no ACL on Windows.
  - Done (phase A, `internal/adapters/audiocapture`): chunks go through `filestore.Create` (exclusive) + `Rename`; the
    final track is one 16 kHz mono 16-bit WAV (windowed-sinc resampling, downmix), 12 h fit the 4 GiB WAV limit.
  - Protocol: one JSON request line on stdin (`v`, recording, chunks, target, chunk seconds, lead silence, floor mode),
    stdin EOF = stop; JSON events on stdout (started, attempt, switch, chunk, stats every 5 s = heartbeat, failed, stopped).
  - Restarts: a crashed child or one silent for 30 s is restarted after 1/2/4 s, at most 3 times, with the missed time
    as lead silence and in the mode it ended in; a reported failure is not restarted. `Wait` then returns `ErrCaptureFailed`.
  - Default target until TASK-059: EXCLUDE of our own process tree; the WASAPI source (phase B) falls back to system.
  - Done (phase B): the Windows WASAPI source (one process loopback activation per child, reopen budget of 5 tries per
    incident), the process checker and the wiring; manual checks in `docs/tasks/TASK-017-recorder-manual-checks.md`.
    Mute detection and storing a mode switch as a problem of the recording are TASK-058.
- **TASK-058 (diagnostics):** log at start (no audio content): Windows build, chosen mode, target exe and PID, output device
  names, stream format, every attempt with its HRESULT, each switch with its reason. Tone self-test: our own tone is not in the
  chosen app's tree, so test INCLUDE of our own PID and classic loopback; it proves the build works, not that the chosen app is
  audible. Report "healthy but mute" and BT HFP as user-readable causes.
  Done (part A): the child reports the Windows version (RtlGetVersion + registry ProductName/DisplayVersion/UBR, build
  ≥ 22000 = Windows 11), the output device name (every attempt and reopen), the format and the switch cause; the parent
  logs "capture started" with all of it. Healthy-but-mute detector in the child (`mute.go`): process loopback, only
  zeros/SILENT packets, meters show sound (INCLUDE: an active, unmuted session of the target tree with peak > 0;
  EXCLUDE: the default endpoint peak) for `SourceConfig.MuteThreshold` (default 15 s) → classic loopback. A muted
  tab/app shows no sound, so it never switches. The `Recorder` port got `Diagnostics` (domain
  `values.CaptureDiagnostics`); the session stores a fallback as the problem of the recording (fixed text) on the next
  heartbeat or before the stop. Not confirmed: whether the session meter reads before or after the session volume
  (mute and volume 0 are checked too).
- **TASK-059 (picker):** list built from top-level windows grouped by exe (as OBS), audio-session state as a "playing now"
  badge (sessions appear only after the first sound); target = tree root PID; Chromium session PIDs walked up to the window
  owner. Remember by exe, not PID; re-find after the app restarts (OBS: every 3 s) and record silence meanwhile; activation
  succeeds for a dead PID, so check liveness. Exclude our own process. Open links via `explorer.exe` so the browser is not in
  our tree. Show the mute advice and the "same browser" hint.
  Done: popup picker (marks, 3 s refresh, advice), remembered by exe, re-bind by exe while recording; checks: `docs/qa/run-alpha.md`.
- **CI:** builds `CGO_ENABLED=0`; runners have no audio device, so live audio checks stay manual (TASK-016, compat matrix).

## Verification (TASK-016, owner, Windows 10 19045)
Reference: **OBS ≥ 28**, source "Application Audio Capture" on the same window, "Desktop Audio" beside it; a result that differs
from OBS is our bug. Each run logs the Windows build, attempts (mode, HRESULT), format, stats (packets, silent, frames, reopens),
CPU and a chunk check at the end. Listen to the `chunk-*.wav` files. Setup (PowerShell, from `paratech/`):
```powershell
go build -o $env:TEMP\cs\capturespike.exe ./desktop/spikes/audio-capture/cmd/capturespike
$cs = "$env:TEMP\cs\capturespike.exe"; $o = "$env:TEMP\cap"
```
1. **Baseline, first start.** Download the exe once through a browser (cloud drive) and start it: note SmartScreen/Defender.
   Speech video in Chrome: `& $cs -mode include -exe chrome.exe -out $o\01i -duration 2m -child`, then
   `& $cs -mode exclude -out $o\01e -duration 1m` and `& $cs -mode system -out $o\01s -duration 1m`.
   Expect: include attempt ok, speech in every chunk, all valid; exclude and system also have it; SmartScreen may warn (note text).
2. **Windows volume 0, then speaker mute**, on each output (speakers, wired headphones, HDMI, BT); then route Chrome to an output
   you do not hear (Settings > Sound > App volume). Parallel: `& $cs -mode include -exe chrome.exe -out $o\02i -duration 1m` and
   `& $cs -mode system -out $o\02s -duration 1m`. Expect: include at full level everywhere (not confirmed); system may drop on some devices.
3. **Mixer and tab mute.** Chrome slider 50 %, 0, muted; tab mute in Chrome, Edge, Firefox, Yandex (exe from Task Manager > Details):
   `& $cs -mode include -exe chrome.exe -out $o\03 -duration 2m` (change `-exe`). Expect: level follows the slider; mute = silence.
   Note whether packets keep coming (the "healthy but mute" pattern).
4. **Another video:** (a) lecture in Chrome, video in Edge; (b) two Chrome tabs; (c) lecture tab muted, other tab plays.
   `& $cs -mode include -exe chrome.exe -out $o\04a -duration 1m` (04b, 04c). Expect: (a) lecture only; (b) both; (c) video only.
5. **Desktop clients:** Zoom (zoom.us/test, Test speaker), new Teams (test call), Telemost with a second device; then speaker off
   in the client; then a Zoom call while a lecture plays in Chrome (ducking; repeat with Sound > Communications > Do nothing).
   `& $cs -mode include -exe Zoom.exe -out $o\05z -duration 1m` (`ms-teams.exe`, Telemost exe from Task Manager, `chrome.exe`).
   Expect: other side audible; speaker off = silence; ducking may lower the lecture.
6. **App restart.** Chrome closed: `& $cs -mode auto -exe chrome.exe -out $o\06a -duration 1m` (expect "include target not found,
   falling back", system mode). Chrome open: `& $cs -mode include -exe chrome.exe -out $o\06b -duration 3m`, close and reopen
   Chrome mid-run. Expect: the spike does not re-find (PID resolved once): silence or a stream error after the restart; OBS re-attaches.
7. **Devices.** Parallel `& $cs -mode include -exe chrome.exe -out $o\07i -duration 5m` and `& $cs -mode system -out $o\07s -duration 5m`:
   change the default output, unplug wired headphones, BT off/on; then a Zoom call with the BT headset mic (HFP),
   `-exe Zoom.exe`. Expect: system log `reopens>0` and keeps writing; include continues (not confirmed); HFP: system likely silent.
8. **Sleep/resume, lock screen, `Restart-Service audiosrv -Force`** (admin) during
   `& $cs -mode include -exe chrome.exe -out $o\08 -duration 10m -child` (and system in parallel). Expect: note resume, error
   (exit code, HRESULT) or silence; closed chunks valid.
9. **Privacy:** Settings > Privacy > Microphone, both toggles off: `& $cs -mode include -exe chrome.exe -out $o\09i -duration 30s`,
   `& $cs -mode system -out $o\09s -duration 30s`. Expect: unknown; note `E_ACCESSDENIED` or normal recording. Turn back on.
10. **3 h run**, long playlist in Chrome: `& $cs -mode include -exe chrome.exe -out $o\10 -chunk 60s -duration 3h -child`.
    Expect: exit 0, 180 valid chunks; frames / 48000 vs 10800 s (note the difference); child CPU ~0.6 % of a core;
    memory (Task Manager, child) flat at 5 min / 1 h / 3 h.
11. **Kill.** `& $cs -mode include -exe chrome.exe -out $o\11 -duration 5m -child`; after two "chunk closed" lines
    `taskkill /F /PID <child_pid>` (from "child started"); then `& $cs -mode include -exe chrome.exe -out $o\11 -duration 1m`.
    Expect: parent exit 1; earlier chunks valid with audio; one `.partial` invalid; the rerun continues numbering, nothing overwritten.

| Step | Result | Notes |
|---|---|---|
| 1 | | |
| 2 | | |
| 3 | | |
| 4 | | |
| 5 | | |
| 6 | | |
| 7 | | |
| 8 | | |
| 9 | | |
| 10 | | |
| 11 | | |

## Sources (details and more in `docs/refs/audio.md`)
- Process loopback: [PROCESS_LOOPBACK_MODE](https://learn.microsoft.com/en-us/windows/win32/api/audioclientactivationparams/ne-audioclientactivationparams-process_loopback_mode),
  [ActivateAudioInterfaceAsync](https://learn.microsoft.com/en-us/windows/win32/api/mmdeviceapi/nf-mmdeviceapi-activateaudiointerfaceasync),
  [ApplicationLoopback sample](https://learn.microsoft.com/en-us/samples/microsoft/windows-classic-samples/applicationloopbackaudio-sample/),
  19041+ in practice: [OBS win-wasapi.cpp](https://github.com/obsproject/obs-studio/blob/master/plugins/win-wasapi/win-wasapi.cpp),
  [GStreamer wasapi2](https://github.com/GStreamer/gstreamer/blob/main/subprojects/gst-plugins-bad/sys/wasapi2/gstwasapi2util.cpp).
- Classic loopback: [Loopback Recording](https://learn.microsoft.com/en-us/windows/win32/coreaudio/loopback-recording),
  [Stream Routing](https://learn.microsoft.com/en-us/windows/win32/coreaudio/stream-routing),
  keep-alive: [GStreamer MR 1588](https://gitlab.freedesktop.org/gstreamer/gst-plugins-bad/-/merge_requests/1588).
- Mute and volume: [AUDCLNT_STREAMOPTIONS](https://learn.microsoft.com/en-us/windows/win32/api/audioclient/ne-audioclient-audclnt_streamoptions),
  Chromium [output_controller.cc](https://chromium.googlesource.com/chromium/src/+/main/services/audio/output_controller.cc),
  [Audio Sessions](https://learn.microsoft.com/en-us/windows/win32/coreaudio/audio-sessions).
- Defects: [MeetCap #38](https://github.com/lybym/MeetCap/issues/38) (zeros on 26200), [MS Q&A 22H2 crash](https://learn.microsoft.com/en-us/answers/a/1038147).
- malgo: [pkg.go.dev](https://pkg.go.dev/github.com/gen2brain/malgo) v0.11.26; [miniaudio CHANGES](https://github.com/mackron/miniaudio/blob/master/CHANGES.md).
