# TASK-372 — E2E mode of the desktop app (build tag `e2e`)

Branch ralph/20261104-task-372. Go only; no frontend, no Hub change.

## What was done
- `config`: `Config.E2E` (Dir, CDPPort, Window) with validation joined into `Validate` (dir absolute; port 1024..65535; window `WxH`,
  each side 640..3840; port/window need `--e2e-dir`). Flags are registered only in `e2e_on.go` (`//go:build e2e`); `e2e_off.go`
  registers none, so the shipped exe answers `--e2e-dir` with «flag provided but not defined». No env var. `config` is std-only.
- `input/wails`: `Options` got `Title`, `BrowserArgs`, `MainWidth/MainHeight`; the title is used for both windows and the tray tooltip;
  `BrowserArgs` goes to `Windows.AdditionalBrowserArgs`. Wails beta.29 facts (module source): `application_options.go:266/283/294`
  (`WindowsOptions`, `WebviewUserDataPath`, `AdditionalBrowserArgs`), `single_instance.go:31` (`UniqueID`), args appended after
  Wails' own `--disable-features=…` (`webview_window_windows.go` ~2481-2505), env var cleared in `webviewloader/env_create.go:169`.
- `app/e2e.go`: `shellOptions(cfg)` (pure): id `paratech.learning-assistant.desktop.e2e`, WebView2 dir `<e2e-dir>/webview`,
  title «paratech · E2E», `--remote-debugging-port=<n>`, window size. `checkE2EDirs` (in `New` and again in `openCore`, before the data
  dir is created and before the lock) refuses the default data dir, anything inside or containing it, and an overlap of the run's
  WebView2 folder with the owner's (by cleaned name, case-folded, and by file identity: links, 8.3 names). `checkE2EProfile` refuses a
  non-isolated Credential Manager profile. `e2e mode on` at info (synthetic paths, port, size; no secrets). `ensureURLScheme` skipped.
- Hub bridge: `loopbackHub`/`loopbackURL` (settingsbridge.go) wrap the Hub URL source; only `localhost`, `127.0.0.1`, `::1` pass,
  anything else reads as «not configured». Wired by `env.loopbackHubOnly` set in `openCore` from `cfg.E2E.On()`.
- `build-alpha.ps1 -E2E` (tags `production,e2e`, default OutDir `paratech/dist/e2e`, exact tags line check — a normal alpha build now
  also fails if it carries `e2e`; `-E2E -Smoke` passes `--e2e-dir` and waits for `e2e mode on`; `-E2E -ComponentOnly` refused).
- CI: `go vet -tags e2e` and `go test -tags e2e ./desktop/...` on the Linux (with -race) and Windows jobs.

## Scope deviation (decide: accepted by the coordinator as routine)
AC6 could not be wired inside the listed files: `internal/app/env.go` (one field) and `core.go` (5 lines at `hubclient.New`) were also
touched. Test `TestE2EHubIsLoopbackOnly` goes through the real `openCore` and the Connections bindings (remote URL: no request leaves,
status `notConfigured`; loopback URL works; normal mode unchanged).

## Manual check on Windows (2026-11-04, isolated temp dir, e2e exe from `build-alpha.ps1 -E2E -SkipWhisper -Smoke`)
- Smoke passed (headless, `e2e mode on`, `secrets_profile=isolated`).
- With the owner's `paratech.exe` (beta, pid 50344) running: the E2E exe started with `--data-dir/--e2e-dir` in `%TEMP%`,
  `--e2e-cdp-port 39222 --e2e-window 1280x800`; both processes stayed up, the E2E window title was «paratech · E2E», the owner's stayed «paratech».
- `curl`-equivalent `http://127.0.0.1:39222/json/version` answered 200; `/json/list` showed `index.html` (main) and `popup.html` pages in
  one environment.
- `netstat -ano`: the port listens on `127.0.0.1:39222` only (loopback), as the task asked to verify.
- `HKCU\Software\Classes\paratech\shell\open\command` identical before and after (owner's beta path). The E2E process was stopped after the check.
- Not checked by hand: the tray tooltip text (same `title()` as the windows, covered by unit tests for the windows only).

## Verification
`go vet` and `go test -race -count=1` of `./desktop/config/...`, `./desktop/internal/app/...`, `./desktop/internal/input/wails/...`
with and without `-tags e2e`; `GOOS=windows go vet [-tags e2e] ./desktop/...`; `GOOS=linux CGO_ENABLED=0 go vet -tags e2e`;
golangci-lint v2.14.0 on the touched packages (default, `--build-tags e2e`, linux): 0 issues; arch tests with and without the tag.
`verify.sh` not run.

## Review
reviewer: approve, no blocker/high/medium. security-auditor: no high/medium. No re-review needed (no blocker/high).

## Follow-ups (lows, not fixed)
- `build-release.ps1:252` (`Get-ReleaseExeProblems`) checks `-tags=production` as a substring, so with `-SkipAppBuild` an e2e exe in the
  app dir would pass; use the anchored regex of build-alpha.ps1 (outside this task's files).
- The credential profile is computed twice (`checkE2EProfile` and `secretsProfile()`); compute once or refuse the default profile inside
  `secretsProfile` in E2E mode (race only for a same-user attacker).
- Default data dir comes from `%LOCALAPPDATA%` (env is trusted input); could use the known-folder API. Loopback SMB share reaching the default dir: `os.SameFile` not verified.
- Reword the comment at `app.go:177` ("Again here…") to the real reason (identity check sees links that exist by now); drop "TASK-372" from a test comment (`e2e_test.go` ~489).
- Symlink/junction refusal test skips on this machine (no symlink privilege); runs on Linux CI.
- CI: `go test -tags e2e ./desktop/...` reruns the whole desktop suite (~9 min app package on Windows); only `config` has tag-specific code, so `./desktop/config/...` would prove the same. Supervisor may narrow it. Optional: `GOOS=windows go vet -tags e2e` in the Linux job.
- Redirects: the Hub client already uses `ErrUseLastResponse`, so a loopback Hub cannot redirect off-machine.

## Changed existing tests
None.

## Security line
Flags only under tag `e2e`; refuses default data dir / non-isolated profile; loopback-only Hub; own Wails id; no scheme registration; CDP bound to
127.0.0.1 (verified); no secrets logged. Auditor: three lows, listed above.
