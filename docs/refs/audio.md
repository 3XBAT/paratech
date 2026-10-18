# audio: system audio capture on Windows (WASAPI loopback, process loopback, audio sessions)

Sources checked: 2026-10-15 (from research reports; nothing re-fetched here).
Libraries: `golang.org/x/sys` v0.48.0 (desktop `go.mod`). `github.com/gen2brain/malgo` v0.11.26 only in the spike's separate module (rejected).
Main docs: https://learn.microsoft.com/en-us/windows/win32/coreaudio/loopback-recording ,
https://learn.microsoft.com/en-us/samples/microsoft/windows-classic-samples/applicationloopbackaudio-sample/
Decision: `docs/adr/0002-audio-capture.md` (ADR-0002, audio capture). Spike code: `paratech/desktop/spikes/audio-capture/`.

Evidence marks: [MS] Microsoft docs, [code] reading of OBS/Chromium/GStreamer source, [forum] user reports or issues,
[spike] measured by the spike on Windows 10 19045, [inference] reasoning only. "Not confirmed" = no primary source.

## Where the GUIDs, vtables and constants come from
- GUIDs, vtable slot order and most constants: MSYS2 mingw-w64 headers (`C:/msys64/mingw64/include`, gcc 14.2.0):
  `mmdeviceapi.h`, `audioclient.h`, `unknwnbase.h`, `objidlbase.h`, `combaseapi.h`, `wtypes.h`, `propidl.h`.
- NOT in those headers (mingw has no `audioclientactivationparams.h`); taken from the Microsoft docs and confirmed only by a
  successful activation on 19045 [spike]: `VAD\Process_Loopback`, `AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK = 1`,
  `PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE = 0`, `PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE = 1`,
  the `AUDIOCLIENT_ACTIVATION_PARAMS` layout.
- `x/sys/windows` v0.48.0 has neither `CoCreateInstance` nor `ActivateAudioInterfaceAsync`: load them with
  `windows.NewLazySystemDLL("ole32.dll")` / `("mmdevapi.dll")` and `.NewProc(...)`.
- Calls: `syscall.SyscallN(vtbl[slot], this, args...)`; IUnknown takes slots 0-2, then methods in header order. 64-bit only:
  `IAudioClient::Initialize` takes two `REFERENCE_TIME` (int64) arguments that a 32-bit call would split.
- Audio-session interfaces (`IAudioSessionManager2`, `IAudioSessionControl2`, `IAudioMeterInformation`, `audiopolicy.h`)
  were NOT used by the spike: TASK-059 takes their GUIDs and slots from the headers itself; the meter: see "Meters".

| GUID | Value |
|---|---|
| CLSID_MMDeviceEnumerator | BCDE0395-E52F-467C-8E3D-C4579291692E |
| IID_IMMDeviceEnumerator | A95664D2-9614-4F35-A746-DE8DB63617E6 |
| IID_IAudioClient | 1CB9AD4C-DBFA-4C32-B178-C2F568A703B2 |
| IID_IAudioCaptureClient | C8ADBD64-E71E-48A0-A4DE-185C395CD317 |
| IID_IAudioRenderClient | F294ACFC-3146-4483-A7BF-ADDCA7C260E2 |
| IID_IAgileObject | 94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90 |
| IID_IActivateAudioInterfaceCompletionHandler | 41D949AB-9862-444A-80F6-C261334DA5EB |
| IID_IPropertyStore (propsys.h) | 886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99 |
| PKEY_Device_FriendlyName (functiondiscoverykeys_devpkey.h) | {A45C254E-DF1C-4EFD-8020-67D146A850E0}, pid 14 |
| PKEY_Device_EnumeratorName (functiondiscoverykeys_devpkey.h) | {A45C254E-DF1C-4EFD-8020-67D146A850E0}, pid 24 |
| PKEY_AudioEndpoint_FormFactor (mmdeviceapi.h; VT_UI4 EndpointFormFactor: Headphones 3, Headset 5, Handset 6) | {1DA5D803-D492-4EDD-8C23-E0C0FFEE7F0E}, pid 0 |
| IID_IAudioMeterInformation (Windows SDK endpointvolume.h; mingw only declares it) | C02216F6-8C67-4B5B-9D00-D008E73E0064 |

| Interface | Slots used (0-2 = IUnknown) |
|---|---|
| IMMDeviceEnumerator | GetDefaultAudioEndpoint 4 |
| IMMDevice | Activate 3, OpenPropertyStore 4, GetId 5 |
| IPropertyStore | GetValue 5 (PROPVARIANT VT_LPWSTR = 31, freed by ole32 `PropVariantClear`; STGM_READ = 0) |
| IAudioMeterInformation | GetPeakValue 3 |
| IAudioClient | Initialize 3, GetBufferSize 4, GetCurrentPadding 6, GetMixFormat 8, Start 10, Stop 11, SetEventHandle 13, GetService 14 |
| IAudioCaptureClient | GetBuffer 3, ReleaseBuffer 4, GetNextPacketSize 5 |
| IAudioRenderClient | GetBuffer 3, ReleaseBuffer 4 |
| IActivateAudioInterfaceAsyncOperation | GetActivateResult 3 |

| Constant | Value |
|---|---|
| AUDCLNT_STREAMFLAGS_LOOPBACK / EVENTCALLBACK | 0x00020000 / 0x00040000 |
| AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY / AUTOCONVERTPCM | 0x08000000 / 0x80000000 |
| AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY / SILENT | 0x1 / 0x2 |
| AUDCLNT_SHAREMODE_SHARED, eRender, eConsole | 0, 0, 0 |
| CLSCTX_ALL | 0x17 |
| VT_BLOB / VT_UI4 / VT_LPWSTR | 65 / 19 / 31 |

## Classic loopback (`AUDCLNT_STREAMFLAGS_LOOPBACK`)
- Capture stream opened on a **render** endpoint, shared mode only, format = endpoint mix format [MS] (Loopback Recording; OBS does
  the same: https://github.com/obsproject/obs-studio/blob/master/plugins/win-wasapi/win-wasapi.cpp).
  Spike: `Initialize(SHARED, LOOPBACK|EVENTCALLBACK, 200 ms, mix format)`; 19045 gave 48000 Hz 2 ch float32 (EXTENSIBLE).
- Event-driven loopback works since Windows 10 1703 [MS].
- DRM content is not delivered by a trusted driver [MS]. Captures the mix of all sessions, including session 0 [MS].
- **No packets while nothing plays.** Not in MS docs; practice: GStreamer writes silence into a render client
  (https://gitlab.freedesktop.org/gstreamer/gst-plugins-bad/-/merge_requests/1588), NAudio `DataAvailable` stops
  (https://github.com/naudio/NAudio/blob/master/Docs/WasapiLoopbackCapture.md), OBS "silent loopback fix" (win-wasapi.cpp).
  Fix: **keep-alive render client** on the same endpoint writing silence (`ReleaseBuffer(n, AUDCLNT_BUFFERFLAGS_SILENT)`).
  [spike] malgo (no keep-alive): 0 callbacks in 5 s of silence; with the spike's keep-alive running: 501.
- `AUDCLNT_BUFFERFLAGS_SILENT` on a captured packet: ignore the data, write zeros (research §2.1, Loopback Recording).
  [spike] the first packet carries DATA_DISCONTINUITY.
- **Device change:** the stream is bound to one endpoint. A direct WASAPI client listens to `IMMNotificationClient` (default
  changed, device removed, format changed) and recreates the stream [MS]
  https://learn.microsoft.com/en-us/windows/win32/coreaudio/stream-routing . Automatic stream routing exists since 1607
  (https://learn.microsoft.com/en-us/windows/win32/coreaudio/automatic-stream-routing); for a loopback stream: not confirmed.
  OBS reopens after 3 s on default change or any `GetBuffer` error [code].
  [spike] polls the default endpoint id every 2 s; on change or `AUDCLNT_E_DEVICE_INVALIDATED` releases everything and reopens
  with backoff 250 ms, 500 ms, 1 s, 2 s, 4 s (5 attempts), then fails. Not exercised with a real change.
- Retry loops on `AUDCLNT_E_DEVICE_INVALIDATED` must be bounded (GStreamer had an endless loop:
  https://gitlab.freedesktop.org/gstreamer/gst-plugins-bad/-/merge_requests/141).
- Exclusive-mode streams of other apps bypass the mixer and probably are not in loopback [inference].
- **Bluetooth, Windows 10:** HFP ("Headset") and A2DP ("Headphones") are two endpoints; when the call app opens the headset mic,
  audio goes to the HFP endpoint and loopback on the stereo endpoint is silent
  ([forum] https://obsproject.com/forum/threads/no-audio-via-bluetooth-headset.152942/ ,
  https://support.steelseries.com/hc/en-us/articles/360057526291-Why-do-I-lose-Bluetooth-audio-when-taking-a-call).
  Windows 11 merges them into one endpoint [MS]
  https://learn.microsoft.com/en-us/windows-hardware/design/accessory-guidelines/bluetooth-accessory-guidelines/bluetooth-accessory-guidelines-classic-audio .
  Matters only for the classic fallback (process loopback is endpoint-independent).
- Sleep/resume: callback may not restart, device "removed"; JUCE waits ~1 s before restart
  ([forum] https://forum.juce.com/t/audio-callback-never-starts-after-waking-from-sleep/44677). Not tested.

## Process loopback (`ActivateAudioInterfaceAsync` + `AUDIOCLIENT_ACTIVATION_PARAMS`)
- `ActivateAudioInterfaceAsync(path, riid, activationParams, handler, &op)` is exported by `Mmdevapi.dll`; OBS gets it via
  `GetProcAddress` [code]. No Windows SDK or C++ needed.
  [MS] https://learn.microsoft.com/en-us/windows/win32/api/mmdeviceapi/nf-mmdeviceapi-activateaudiointerfaceasync
- `path` = `VAD\Process_Loopback` (`VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK`), `riid` = IID_IAudioClient.
- `activationParams` = PROPVARIANT, `vt = VT_BLOB (65)`, three reserved WORDs, then BLOB `{ULONG cbSize; BYTE *pBlobData}`
  (24 bytes on 64-bit). Blob = `AUDIOCLIENT_ACTIVATION_PARAMS` = 3 DWORDs:
  `ActivationType` (1 = PROCESS_LOOPBACK), `TargetProcessId`, `ProcessLoopbackMode` (0 = INCLUDE tree, 1 = EXCLUDE tree)
  [MS] https://learn.microsoft.com/en-us/windows/win32/api/audioclientactivationparams/ns-audioclientactivationparams-audioclient_process_loopback_params ,
  https://learn.microsoft.com/en-us/windows/win32/api/audioclientactivationparams/ne-audioclientactivationparams-process_loopback_mode .
  Keep the PROPVARIANT and blob reachable until completion (Windows may read them asynchronously).
- **Completion handler must be agile**: implements IUnknown + `IAgileObject` + `IActivateAudioInterfaceCompletionHandler`
  (`ActivateCompleted(op)`, slot 3); otherwise `E_ILLEGAL_METHOD_CALL` on older OS. The callback runs on an MTA thread [MS].
  Then `IActivateAudioInterfaceAsyncOperation::GetActivateResult(&hr, &unk)`; `E_ACCESSDENIED` there = no consent [MS]
  https://learn.microsoft.com/en-us/windows/desktop/api/mmdeviceapi/nf-mmdeviceapi-iactivateaudiointerfaceasyncoperation-getactivateresult .
  [spike] handler = Go struct with the vtable pointer first, `ActivateCompleted` only sets an event; wait in 100 ms steps, 5 s timeout.
- **Explicit format:** `GetMixFormat` and `IsFormatSupported` return `E_NOTIMPL` (the client is `AudioSes!CMixerClient`);
  the app chooses the format. MS calls 44.1 kHz/16-bit/stereo "safe" ([MS Q&A] https://learn.microsoft.com/en-us/answers/a/1183153);
  NAudio: no mix format, request one explicitly (https://naudio.github.io/NAudio/docs/WasapiRecorder.html).
  [spike] flags `LOOPBACK|EVENTCALLBACK|AUTOCONVERTPCM|SRC_DEFAULT_QUALITY`; 48000 Hz float32 stereo accepted on the first try
  in INCLUDE and EXCLUDE (fallback 44100 Hz s16 stereo never needed). 16 kHz mono: not confirmed.
- **Endpoint-independent:** no client per device [MS sample]. Follows device changes by itself: logical, not confirmed.
- Target without render streams: silence [MS sample]. [spike] packets arrive continuously in silence without any render stream
  (600 per 6 s, 800 per 8 s on 19045). Conflicts with NAudio ("no buffers while nothing plays"): design for both.
- [spike] Activation **succeeds for a nonexistent PID** and then delivers silence: activation success does not prove the target lives.
- `DevicePosition` is 0 for process loopback; build the timeline from frames and QPC
  ([forum] https://github.com/lybym/MeetCap/pull/35).
- INCLUDE captures the target process and its descendants [MS PROCESS_LOOPBACK_MODE]. The tree follows the parent at creation:
  a browser started by our app via ShellExecute became our child and dropped out of EXCLUDE
  ([code] https://github.com/OpenWhispr/openwhispr/pull/1864); open links via `explorer.exe`.
- **Minimum build, sources conflict:**

| Source | Build |
|---|---|
| [MS] PROCESS_LOOPBACK_MODE, AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS, sample | 20348 |
| [MS] ActivateAudioInterfaceAsync page | 20438 ("Starting with Windows 10 Build 20438") |
| [code] OBS `plugin-main.cpp` ("MS says 20348, but process filtering seems to work earlier") | 19041 |
| [code] GStreamer `gst_wasapi2_can_process_loopback` (https://github.com/GStreamer/gstreamer/blob/main/subprojects/gst-plugins-bad/sys/wasapi2/gstwasapi2util.cpp) | 19041 |
| NAudio docs; AudioLoopbackRecorder README (https://github.com/CingZeoi/AudioLoopbackRecorder) | 19041 |
| win-capture-audio README (https://github.com/bozbez/win-capture-audio) | 2004 + recent updates |
| [spike] this project | activated on 19045 (INCLUDE and EXCLUDE) |

## Audio sessions (TASK-059 picker)
- Enumerate: `IMMDeviceEnumerator::EnumAudioEndpoints(eRender, ACTIVE)` → per device `IAudioSessionManager2::GetSessionEnumerator`
  → `IAudioSessionControl2`: `GetProcessId`, `GetDisplayName`, `GetIconPath`, `GetState` (active/inactive/expired),
  `IsSystemSoundsSession`. A session belongs to one device [MS] https://learn.microsoft.com/en-us/windows/win32/coreaudio/audio-sessions .
- `GetProcessId` may return `AUDCLNT_S_NO_SINGLE_PROCESS` (cross-process session, PID of the creator) [MS]
  https://learn.microsoft.com/en-us/windows/win32/api/audiopolicy/nf-audiopolicy-iaudiosessioncontrol2-getprocessid .
- `OnSessionCreated` arrives only after `IAudioSessionEnumerator::GetCount` and only in MTA [MS]
  https://learn.microsoft.com/en-us/windows/win32/api/audiopolicy/nf-audiopolicy-iaudiosessionmanager2-registersessionnotification .
- **A session appears only after the first sound** and expires after idle [MS audio-sessions]: a session-only list misses Zoom
  before it plays. Build the list from windows; sessions only give the "playing now" badge.
- Windows like OBS ([code] https://github.com/obsproject/obs-studio/blob/master/libobs/util/windows/window-helpers.c): visible,
  no `WS_EX_TOOLWINDOW`/`WS_CHILD`; replace `ApplicationFrameWindow` by its child of another process; skip
  `applicationframehost.exe`, `shellexperiencehost.exe`, untitled `explorer.exe`; exe via `GetProcessImageFileNameW`;
  label "[exe]: title". OBS target = `GetWindowThreadProcessId` of the window + INCLUDE tree; when the window is gone it
  re-finds by title, class and exe every 3 s (win-wasapi.cpp); no tree toggle (https://github.com/obsproject/obs-studio/issues/9669).
- Chromium's audio is rendered by a separate "Utility: Audio Service" child process
  ([code] https://chromium.googlesource.com/chromium/src/+/main/services/audio/README.md): session PID ≠ window PID
  (not confirmed). Walk up `th32ParentProcessID` (Toolhelp32) to the process with a window; check the parent's creation time
  because PIDs are reused (https://devblogs.microsoft.com/oldnewthing/20150403-00/?p=44313).
- [spike] `TreeRoot` picks, for an exe name, the same-name process whose parent has another name, most descendants, then lowest
  PID. Heuristic only; the product starts from windows.

## Meters and device names (TASK-058)
- Endpoint meter: `IMMDevice::Activate(IID_IAudioMeterInformation)`; session meter: `QueryInterface` of
  `IAudioSessionControl` for `IAudioMeterInformation` (as NAudio does). `GetPeakValue` gives 0..1. The meter IID is not in
  the mingw-w64 headers; a one-off check on 19045 (2026-10-16, nothing played or captured) got both meters with it. A wrong IID would only give E_NOINTERFACE: no sound shown, no switch.
- Whether the session meter reads before or after the session volume/mute: not confirmed; the detector also requires
  `ISimpleAudioVolume` not muted and volume > 0, and a peak above 1e-4 (-80 dBFS).
- The healthy-but-mute detector reads only session meters, for EXCLUDE those outside our own process tree. The endpoint
  meter is not used: it read above zero with nothing playing (TASK-058 review, 2026-10-16), and it also shows our own
  tree and the device's processing, which EXCLUDE does not capture.
- Device name: `IMMDevice::OpenPropertyStore(STGM_READ)` → `GetValue(PKEY_Device_FriendlyName)`; localized
  ("Динамики (Realtek(R) Audio)" on the owner's machine). User hardware names, fine to log.
- "Check sound" (TASK-075) plays nothing: it captures the chosen source and shows its peak level. The tone render
  client and the Bluetooth hands-free hint of TASK-058 were removed.
- (Removed with the hint, kept for reference) Bluetooth hands-free: form factor Headset or Handset
  (`PKEY_AudioEndpoint_FormFactor`) and an enumerator name starting with `BTH` (`PKEY_Device_EnumeratorName`); **not
  confirmed** (an endpoint may report the software-device enumerator `SWD` instead).
- Windows version: `RtlGetVersion` for the numbers; `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion` ProductName,
  DisplayVersion, UBR read only. ProductName says "Windows 10" on Windows 11; build ≥ 22000 (not Server) = Windows 11.

## Volume, mute, ducking (where the tap is)
- Loopback default is tapped **before** endpoint volume/mute; `AUDCLNT_STREAMOPTIONS_POST_VOLUME_LOOPBACK` (via
  `SetClientProperties`) asks for post-volume [MS]
  https://learn.microsoft.com/en-us/windows/win32/api/audioclient/ne-audioclient-audclnt_streamoptions .
- Driver side `KSPROPERTY_AUDIOLOOPBACK`: pre or post volume/mute; pre if absent; mandatory from Windows 11 24H2 for hardware
  audio engines [MS] https://learn.microsoft.com/en-us/windows-hardware/drivers/audio/ksproperty-audioloopback .
- Practice differs for classic loopback: volume/mute affect capture on some devices (HDMI, Razer USB, BT A2DP, Focusrite, some
  Win11 laptops) [forum] https://github.com/filoe/cscore/issues/86 ,
  https://obsproject.com/forum/threads/how-to-make-obs-capture-full-windows-volume-despite-muted.162766/ ,
  https://obsproject.com/forum/threads/bypass-windows-volume-level-in-capture.176825/ ,
  https://obsproject.com/forum/threads/capture-window-audio-although-system-sound-is-muted.148735/ .
- Process loopback vs endpoint volume: not bound to the endpoint, so endpoint volume should apply later [inference, not confirmed];
  one OBS user (Win11, OBS 32.1) reports Application Audio Capture is unaffected (thread 162766).
- **Session volume/mute (mixer slider) applies before the tap** in classic and process loopback: muting the app in the mixer
  silences OBS Application Audio Capture, lowering it lowers the level [forum]
  https://obsproject.com/forum/threads/how-do-i-mute-audio-from-a-certain-window-while-still-making-it-so-you-can-hear-in-the-recording.163301/ , thread 176825.
- Ducking: a communication stream (Zoom, Teams) attenuates other streams by 80 % by default [MS]
  https://learn.microsoft.com/en-us/windows/win32/coreaudio/stream-attenuation ; whether it reaches process loopback: not confirmed (likely).

## Browser and app behaviour
- **Chromium tab mute closes the real output stream** and replaces it with a fake one that plays nowhere
  (`OutputController::StartMuting`) [code]
  https://chromium.googlesource.com/chromium/src/+/main/services/audio/output_controller.cc ,
  https://chromium.googlesource.com/chromium/src/+/main/services/audio/local_muter.cc : WASAPI gets nothing, no workaround.
  WebRTC playback (Meet, Telemost in a browser) on the same path [inference]. Yandex Browser: same as Chrome, not confirmed.
- Firefox ≥ 102 closes the system stream when a media element is muted [code]
  https://bugzilla.mozilla.org/show_bug.cgi?id=1743834 ; tab mute and WebRTC: not confirmed.
- All tabs of a Chromium browser render through one process tree: INCLUDE on the browser records every tab
  ([forum] https://obsproject.com/forum/threads/recording-the-audio-of-only-1-tab-alone.155818/).
  Per-tab isolation only via an extension (`chrome.tabCapture`, https://developer.chrome.com/docs/extensions/reference/api/tabCapture); not in MVP.
- Speaker-off inside Zoom/Teams/Telemost: implementation undocumented; expect silence [inference].
- New Teams (WebView2) was silent in OBS Application Audio Capture in 2023
  ([forum] https://obsproject.com/forum/threads/application-audio-capture-beta-doesnt-work-in-new-teams-app.171446/); calls now in
  child `ms-teams_modulehost.exe` (secondary source), covered by the tree: not confirmed. Zoom rendering process, Telemost
  desktop base: not confirmed.
- Chrome opened before OBS sometimes not captured until restarted; cause unknown
  ([forum] https://obsproject.com/forum/threads/possible-issue-with-chrome-browser-being-used-as-an-application-audio-source.193082/).

## Go: callbacks and threads
- `syscall.NewCallback` / `windows.NewCallback`: arguments up to uintptr size, number of callbacks limited (≥ 1024 guaranteed),
  never freed (https://github.com/golang/go/blob/master/src/syscall/syscall_windows.go). Create each callback once per process
  (spike: `sync.OnceValue` vtable shared by all handlers).
- Callbacks run on Windows-created (MTA) threads; no cgo needed (go-wca `IMMNotificationClient` does this:
  https://github.com/moutend/go-wca/blob/develop/pkg/wca/IMMNotificationClient_windows.go). Store into Windows memory through
  `*uintptr`, not typed pointers (GC write barrier).
- Capture thread [spike]: `runtime.LockOSThread` + `CoInitializeEx(MULTITHREADED)`; release in reverse order, free CoTaskMem
  (`GetMixFormat`, `GetId`), close the event handle last, `CoUninitialize`, unlock; every wait ≤ 100 ms so Stop is bounded.

## Go libraries

| Library | CGO | License | Status | Loopback / process loopback |
|---|---|---|---|---|
| github.com/gen2brain/malgo v0.11.26 (miniaudio) | yes | Unlicense | active, commit 2026-08-18 (https://pkg.go.dev/github.com/gen2brain/malgo) | `DeviceType Loopback` yes; `WasapiDeviceConfig` has no process loopback; miniaudio CHANGES has rerouting crash fixes 0.11.12/.22/.23 (https://github.com/mackron/miniaudio/blob/master/CHANGES.md); no keep-alive |
| github.com/moutend/go-wca | no (go-ole v1.2.6) | MIT | abandoned: last commit 2023-03-04, vtable fix PRs #18/#19/#23 unmerged (https://github.com/moutend/go-wca/issues) | classic loopback examples (no LockOSThread, forces PCM tag over float, no device change, swallows `GetBuffer` errors); no process loopback |
| gordonklaus/portaudio | yes | - | not checked | excluded by CGO |
| pure-Go process loopback | - | - | none found | - |

## Test environments
- GitHub runners: `windows-2022` = 10.0.20348, `windows-2025` (= `windows-latest`) = 10.0.26100, `windows-11-arm` = 10.0.26200;
  **no audio device**; no hosted Windows 10 (https://github.com/actions/runner-images,
  https://github.com/actions/runner-images/blob/main/images/windows/Windows2022-Readme.md). A virtual cable can be installed in a
  workflow step (https://github.com/AlekseyMartynov/action-vbcable-win, https://github.com/actions/runner-images/issues/6983);
  that loopback then returns a tone: not confirmed.
- Windows 10 19045: only the owner's machine. Windows 11: Hyper-V Gen2 VM with an evaluation ISO + VB-CABLE (research plan).

## Known defects
- Windows 11 **26200**: process loopback activates but returns pure zeros while classic loopback works (NAudio 3.1.0; cause unknown)
  [forum] https://github.com/lybym/MeetCap/issues/38 . Detect "healthy but mute" and fall back.
- Windows 11 22H2: crash on an invalid `GetBuffer` pointer when a game leaves exclusive fullscreen; race in the MS sample's
  `StopCaptureAsync`; no official fix [MS Q&A] https://learn.microsoft.com/en-us/answers/a/1038147 .
- Windows 10 Bluetooth: two endpoints (HFP / A2DP), see Classic loopback.
- Microphone privacy toggle vs loopback / process loopback: not confirmed, sources conflict
  (https://forum.audacityteam.org/t/i-cant-recording-on-winsows-11-using-audacity-3-7-4/145511). Admin rights: not needed by either
  API in practice (OBS, Audacity); no explicit MS statement.
- AV/SmartScreen: unsigned exes get the strongest warning; EV certificates no longer give instant reputation
  (https://textslashplain.com/2024/11/15/best-practices-for-smartscreen-apprep/); Go binaries see AV false positives
  (https://go.dev/doc/faq#virus).
