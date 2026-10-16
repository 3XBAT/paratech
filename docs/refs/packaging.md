# Packaging: Windows installer (NSIS)

Tools: **NSIS 3.x** (`makensis`), templates and WebView2 bootstrapper from **Wails v3.0.0-beta.29** (`$GOMODCACHE/github.com/wailsapp/wails/v3@v3.0.0-beta.29`, below "the module").
NSIS docs: https://nsis.sourceforge.io/Docs/ · Download: https://nsis.sourceforge.io/Download
Files: `paratech/desktop/build-release.ps1`, `paratech/desktop/VERSION`, `paratech/desktop/build/windows/nsis/project.nsi`.
Checked on 2026-10-16 on Windows 10 22H2 **without NSIS**: the script's help, dry run and failure paths ran; everything that needs `makensis`
(compiling, pages, texts, install, uninstall) is **[not verified]** until the owner builds it.

## Why makensis directly
The `wails3` CLI is not used. Its Windows Taskfile (`internal/commands/build_assets/windows/Taskfile.yml`, task `create:nsis:installer`) only runs
`wails3 generate webview2bootstrapper` and `makensis -DARG_WAILS_AMD64_BINARY=... project.nsi`. The script does the same with `project.nsi`,
adapted from `internal/commands/build_assets/windows/nsis/project.nsi.tmpl` and `internal/commands/updatable_build_assets/windows/nsis/wails_tools.nsh.tmpl` (MIT).
NSIS 3 is needed (`Unicode true`, `ManifestDPIAware`, System type `p`); the oldest working 3.x is **[not verified]**: take the current release.

## Version
`desktop/VERSION` holds `X.Y.Z`; `-Version` overrides it. If HEAD has a `vX.Y.Z` tag it must equal the version, else the script fails
(the mismatch path was not run locally). The version goes into the installer name, its file version (`VIProductVersion X.Y.Z.0`) and
`DisplayVersion` in "Apps & features". The exe does not get VERSION: its build info has module version `(devel)` and the git revision
(`internal/adapters/sysinfo`, shown in `about.json` of the support bundle). Injecting VERSION into the exe is a follow-up.

## Build
- `build-release.ps1` calls `build-alpha.ps1 -OutDir paratech\dist\release\<version>\app`: `npm ci`, `npm run build`,
  `go build -tags production -trimpath`, `CGO_ENABLED=0`, windows/amd64, then the `go version -m` check.
- `-tags production` makes Wails ignore `FRONTEND_DEVSERVER_URL` (`internal/assetserver/build_dev.go` is the non-production file); the script
  also clears it, `GOFLAGS`, `GOAMD64`, `GOEXPERIMENT` and `GOWORK` for the build (an unset `GOWORK` still finds `paratech\go.work`).
- After the build and with `-SkipAppBuild` the script runs `go version -m` on the staged exe: it needs `-tags=production`, `CGO_ENABLED=0` and,
  if present, `GOAMD64=v1` (testers have older CPUs). A `GOAMD64` set by `go env -w` is caught here.
- The plan prints the short HEAD commit and whether the tree is dirty; a `vX.Y.Z` tag on a dirty tree gives a WARNING (not an error).
- The exe is a console program, like the alpha: started from the Start menu it opens a console with the log, and closing that console ends the app.
  Since TASK-058 the log is also in `%LOCALAPPDATA%\LearningAssistant\logs\desktop.log`, so `-H windowsgui` (as in the Wails Taskfile above) is possible;
  not done here: it changes `build-alpha.ps1` and needs a check that the `--recorder` child and the smoke run still work (follow-up, owner decision).

## WebView2
- The installer embeds `MicrosoftEdgeWebview2Setup.exe` from the module (`internal/commands/webview2/`, embedded by `internal/commands/generate_webview2.go`;
  1 793 816 bytes, file version 1.3.143.57, valid Authenticode signature of Microsoft Corporation, checked locally). `go.sum` is checked only when
  the module is downloaded, not when it is read from the cache later; at build time the guard is the required Microsoft signature. Nothing is downloaded by the script.
- At install it checks the `pv` value of `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}` under `HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients`
  and `HKCU\Software\Microsoft\EdgeUpdate\Clients` (Wails macro `wails.webview2runtime`). On this Windows 10 the HKLM value is present (155.0.4283.45):
  Windows 10/11 normally have the Evergreen runtime.
- If missing: `/silent /install` of the bootstrapper, which downloads the runtime from Microsoft (needs internet; per-user install without admin **[not verified]**).
  Still missing: a message with https://developer.microsoft.com/microsoft-edge/webview2/ and the install continues; the app then cannot open
  windows (Wails logs `error getting WebView2 version`, `pkg/application/webview_window_windows.go`).

## Install
- Per user, no admin and no UAC: `RequestExecutionLevel user`, `SetShellVarContext current`.
- Folder `%LOCALAPPDATA%\Programs\LearningAssistant` (can be changed on the folder page; an upgrade reuses `InstallLocation`).
- Files: `learning-assistant.exe`, `uninstall.exe`; with `-WhisperDir` also `whisper-cli.exe`, `whisper.dll`, `ggml.dll`, `ggml-base.dll`, `ggml-cpu-*.dll`.
- Start menu `Learning Assistant.lnk`; no desktop shortcut, autostart or file associations; registers the per-user `paratech:` URL scheme (`HKCU\Software\Classes\paratech`, ADR-0013). Uninstall entry `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\LearningAssistant`.
- Windows 10+ and x64 only (Wails `wails.checkArchitecture`). NSIS default icon (an app icon is a follow-up).
- Upgrade = run the new installer over the old one; data is not touched.
- **No auto-update**: nothing checks or downloads updates (PRD §4.3).

## whisper-cli
whisper-cli is looked up next to the exe (`internal/app/env.go`). Without `-WhisperDir` the installer has none: copy the files from the `Release\`
folder of `whisper-bin-x64.zip` (b5454; verify the zip SHA-256 first, `docs/qa/run-alpha.md` §6) into the install folder and restart the app.
With `-WhisperDir` the script first removes whisper files left in the staging folder (no stale `ggml-cpu-*.dll`), then prints the SHA-256 of every
copied file; it does not pin them (follow-up, ADR-0009).
Whether the b5454 binaries need the VC++ Redistributable (`VCRUNTIME140.dll`, `MSVCP140.dll`) is **[not verified]**; the installer does not
ship it. Check on a clean Windows (`docs/qa/windows-checklist.md`, "Инсталлятор").

## Running app
Installer and uninstaller check the Wails single-instance mutex `wails-app-paratech.learning-assistant.desktop-sim`
(`pkg/application/single_instance_windows.go` + `instanceID` in `internal/app/app.go`) and ask to quit from the tray (Retry/Cancel; `/S`: exit code 2).
`--headless` and `--recorder` processes do not hold it; then a locked file gives Retry/Cancel (`AllowSkipFiles off`), and the uninstaller stops
if it cannot delete the exe.

## Uninstall and user data
- Deletes only its files (names above), `uninstall.exe`, the shortcut, the HKCU uninstall key and `HKCU\Software\Classes\paratech`, then `RMDir` **without** `/r` (the Wails template uses `/r`;
  a chosen folder may hold other files).
- Asks (default and `/S`: keep) whether to delete `%LOCALAPPDATA%\LearningAssistant` (recordings, transcripts, models, exports, logs, the database
  with the settings; the same list as the uninstaller message: `learning-assistant.db`, `artifacts\` and `logs\` of `internal/app/core.go`) and the
  WebView2 folder `%AppData%\learning-assistant.exe` (`internal/webview2/pkg/edge/chromium.go`). A `--data-dir` / `LA_DATA_DIR` folder is not touched.
- Credential Manager keeps `paratech/hub/api-key`, `paratech/mail/smtp-password`, `paratech/google/client-id`, `.../client-secret`, `.../refresh-token`.
  Before uninstalling, "Выйти" in "Google Календарь" revokes Google access. Then remove the entries by hand: Control Panel → Credential Manager →
  Windows Credentials (`cmdkey /list | findstr paratech` lists them).

## Code signing (optional, not in the script)
No certificates or passwords in the repo. With your own certificate and the Windows SDK `signtool` **[not verified: no certificate here]**:
1. `build-release.ps1` once; `signtool sign /fd SHA256 /tr <timestamp URL> /td SHA256 /f <cert.pfx> paratech\dist\release\<version>\app\learning-assistant.exe`
2. `build-release.ps1 -SkipAppBuild` (packs the signed exe), then the same `signtool sign` on the installer.
The uninstaller stays unsigned (NSIS `!uninstfinalize` could sign it; follow-up).

## Owner steps (Windows, PowerShell, repository root)
1. Install NSIS 3.x from https://nsis.sourceforge.io/Download, open a new terminal.
2. Optional: whisper.cpp `whisper-bin-x64.zip` (b5454), check its SHA-256, unpack.
3. Plan: `powershell -NoProfile -ExecutionPolicy Bypass -File .\paratech\desktop\build-release.ps1 -DryRun` — makensis, go, npm and WebView2 must be found.
4. Build: same command without `-DryRun`, optionally `-WhisperDir <unpacked>\Release`.
5. Result: `paratech\dist\release\learning-assistant-<version>-windows-amd64-setup.exe` (the script prints size and SHA-256).
6. Install and check: `docs/qa/windows-checklist.md`, section "Инсталлятор".
New release: change `desktop/VERSION`, commit, optionally tag `vX.Y.Z`.
