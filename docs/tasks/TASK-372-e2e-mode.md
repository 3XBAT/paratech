# TASK-372: E2E mode of the desktop app (build tag `e2e`)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** none (runs in parallel with TASK-374)
- **PRD sections / AC:** ADR-0035 §3; ADR-0003, ADR-0007, ADR-0013, ADR-0014; PRD §12 (sandbox of secrets)
- **Агент:** нет: тестовая инфраструктура, не действие студента
- **Branch:** ralph/<date>-task-372

## Goal
A build with tag `e2e` can start the real app in a sandbox that never touches the owner's app, secrets, registry,
WebView2 profile or Hub, and exposes the WebView2 CDP port.

## Allowed to change
- `paratech/desktop/config/{config.go,e2e_on.go,e2e_off.go}` and their tests
- `paratech/desktop/internal/app/{app.go,e2e.go,settingsbridge.go}` and tests
- `paratech/desktop/internal/input/wails/{shell.go,shell_windows.go,shell_other.go}` and tests
- `paratech/desktop/build-alpha.ps1` (switch `-E2E`)
- `.github/workflows/ci.yml` (`go vet -tags e2e` and `go test -tags e2e` of `./desktop/...`, Linux and Windows jobs)

## Out of scope
- Pickers, opener, clipboard overrides (TASK-373); the runner (TASK-375).

## Acceptance criteria
- [ ] Without tag `e2e` no `--e2e-*` flag exists (`--e2e-dir` is «flag provided but not defined», exit 2); a config test
      with and without the tag proves it.
- [ ] With the tag: `--e2e-dir <abs>` (required for the others), `--e2e-cdp-port <1024..65535>`, `--e2e-window <WxH>`
      (each 640..3840); validation errors are joined as in `Validate`. `config` still imports only std (ARCH-52).
- [ ] In E2E mode: Wails `UniqueID` = `paratech.learning-assistant.desktop.e2e`; `WebviewUserDataPath` =
      `<e2e-dir>/webview`; `Windows.AdditionalBrowserArgs` has `--remote-debugging-port=<n>` (not the env var: Wails
      clears it, ADR-0035 context); main window size from `--e2e-window`; title and tray tooltip end with « · E2E».
- [ ] In E2E mode `ensureURLScheme` is not called (test with the fake `env.scheme`: zero calls).
- [ ] In E2E mode `openCore` fails with a clear error when the data dir is the default one or `ProfileFor` is not
      isolated; logs `e2e mode on` at info.
- [ ] In E2E mode a Hub URL whose host is not loopback (`127.0.0.1`, `::1`, `localhost`) is treated as not configured
      by the Hub bridge (jobs park as today); unit test.
- [ ] `build-alpha.ps1 -E2E` builds `-tags production,e2e` into `paratech/dist/e2e` (default OutDir), checks
      `-tags=production,e2e` in `go version -m`; `-E2E -Smoke` passes with `--e2e-dir` in the smoke data folder.
- [ ] Manual check on Windows (record in progress): the E2E exe starts while the owner's paratech runs, both windows
      stay, `curl http://127.0.0.1:<port>/json/version` answers, `HKCU\Software\Classes\paratech` unchanged.
- [ ] `go vet` and `go test -race` (with and without `-tags e2e`) of `./desktop/...`, `GOOS=windows go vet -tags e2e`,
      golangci-lint v2.14.0 pass.

## Notes
Whether Chromium binds the debug port to loopback only is to verify with `netstat -ano` and written to progress.
The `e2e` files carry no secrets and no behaviour beyond the table of ADR-0035 §3.
