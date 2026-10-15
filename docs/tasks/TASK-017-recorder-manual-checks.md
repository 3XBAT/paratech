# TASK-017: manual checks of the recorder on a real Windows PC

Automated tests never touch an audio device. These checks do; run them on the owner's PC (Windows 10 19045).
Real-signal checks (what is and is not recorded) are the 11 steps of ADR-0002 (audio capture), section Verification (TASK-016).

## 1. Live test of the WASAPI source (plays nothing)
```powershell
$env:LA_AUDIO_LIVE_TEST=1; go -C paratech test -run Live -v ./desktop/internal/adapters/audiocapture/; Remove-Item Env:LA_AUDIO_LIVE_TEST
```
Expect: `exclude` 48000 Hz 2 ch 32-bit float, `system` in the mix format, build `10.0.19045`, packets > 0, PASS.

## 2. The app
Build and start it from a console, so the log (stderr) stays visible:
```powershell
go -C paratech build -o $env:TEMP\la\la.exe ./desktop/cmd/desktop; & $env:TEMP\la\la.exe 2>&1 | Tee-Object $env:TEMP\la\la.log
```
Chunks: `%LOCALAPPDATA%\LearningAssistant\artifacts\recordings\<id>\`. Play some audio during the checks.

| # | Do | Expect |
|---|---|---|
| 1 | Tray > "Show recording popup" > Start | Task Manager > Details: a second `la.exe` (command line `--recorder --data-dir ...`); log `capture running` with mode `exclude` |
| 2 | Wait 2-3 min | a `chunk-NNNNNN.wav` every 60 s, `.partial` only for the current one |
| 3 | Stop it in the main window | `recordings\<id>.wav`, 16 kHz mono, plays, length = recording time |
| 4 | Start, then `taskkill /F /PID <child pid>` | log `restarting the recorder child`; numbering continues; the gap is silence in the track |
| 5 | Kill the child 4 times | after the 4th the recording ends with a reason (COMPLETED with a problem or FAILED), no crash |
| 6 | Start, then kill `la.exe` (parent) | the child closes its last chunk and exits within ~3 s; closed chunks are valid; the next start recovers the recording (a restart within those ~3 s may build the track without the last chunk; it stays on disk) |
| 7 | During a recording change the default output device, unplug headphones | it keeps writing; in mode `system` the log shows reopens > 0 (process loopback should follow the device by itself: not confirmed) |

Mode `system` is used only when process loopback fails (log `capture mode switched`); the UI cannot choose it yet (TASK-058/059).
