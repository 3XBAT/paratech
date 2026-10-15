# Wails v3

Library: `github.com/wailsapp/wails/v3` **v3.0.0-beta.29** (latest published v3 tag on 2026-10-14; `go list -m -versions`), pinned in `paratech/desktop/go.mod`.
Checked in the module source (`$GOMODCACHE/github.com/wailsapp/wails/v3@v3.0.0-beta.29`) and on Windows 10 22H2 with WebView2 155.
Source: https://github.com/wailsapp/wails/tree/v3.0.0-beta.29/v3 · API: https://pkg.go.dev/github.com/wailsapp/wails/v3@v3.0.0-beta.29/pkg/application

## Build
- Windows backend is pure Go (`internal/webview2`, `pkg/w32`): `CGO_ENABLED=0 go build` works and the binary runs. Linux/macOS backends need CGO
  (GTK/WebKit, Cocoa), so the project imports Wails only from `*_windows.go` files.
- Without `-tags production` the asset server proxies to `FRONTEND_DEVSERVER_URL` if that env var is set (`internal/assetserver/build_dev.go`)
  and the debug runtime is used. Release builds must use `-tags production`.

## Lifecycle (`pkg/application/application.go`)
- `init()` in `init_desktop.go` calls `runtime.LockOSThread()`: `application.New` and `App.Run` must run on the **main goroutine**.
- `application.New` is a process singleton (`globalApplication`); a second call returns the first app. `Run` works once.
- `App.Quit()` is a no-op until `Run` created the platform app (`a.impl`); wait for `events.Common.ApplicationStarted` before calling it.
  After `Quit`, `Run` returns `nil` (`application_windows.go`: `destroy` → `PostQuitMessage`).
- The default signal handler calls `Quit` on SIGINT/SIGTERM; set `DisableDefaultSignalHandler: true` when `main` owns signals.
- Many failures call `os.Exit(1)` inside Wails (`handleFatalError`), e.g. a window that cannot create WebView2.
- `Windows.DisableQuitOnLastWindowClosed: true` keeps a tray app alive when windows are closed or hidden.
- WebView2 data defaults to `%AppData%\<exe name>` (`internal/webview2/pkg/edge/chromium.go`); override with `Windows.WebviewUserDataPath`.
- `Options.Logger` gets every binding call with its JSON args and result at Debug (`messageprocessor_call.go`), so the shell gives Wails
  a logger with an Info floor (`input/wails/wailslog.go`): the mail password goes through a binding.

## Windows (`webview_window_options.go`, `webview_window_windows.go`)
- `WebviewWindowOptions`: `Frameless`, `AlwaysOnTop` (adds `WS_EX_TOPMOST`), `Hidden` (created without `WS_VISIBLE`), `DisableResize`,
  `Windows.HiddenOnTaskbar` (`WS_EX_TOOLWINDOW`).
- There is no "no activate" option. Wails dropped `WS_EX_NOACTIVATE` from `HiddenOnTaskbar`. `Windows.ExStyle != 0` **replaces** the computed
  extended style (including `WS_EX_CONTROLPARENT` and `WS_EX_TOPMOST`), so list every bit.
- `Show()` uses `ShowWindow(SW_SHOW)`, which activates the window even with `WS_EX_NOACTIVATE` (measured: foreground moved to the popup).
  Calling `w32.ShowWindow(hwnd, SW_SHOWNOACTIVATE)` inside `application.InvokeSync` first, then `Show()`, keeps the foreground (measured).
  `Show()` is still needed: it makes the WebView controller visible.
- Creating a window activates it once: the frameless path calls `SetWindowPos(..., SWP_FRAMECHANGED)` without `SWP_NOACTIVATE`, so a hidden
  popup can become the (invisible) foreground window at startup. Rewriting flags in `WM_WINDOWPOSCHANGING` via `WndProcInterceptor` does not help.
- The zero `BackgroundColour` is transparent black: set an opaque colour or pages without a CSS background render black.
- `w32.WINDOWPOS` lacks the leading `hwnd` field of the Win32 struct; do not use it to read `WM_WINDOWPOSCHANGING`.
- Hide on close: `w.RegisterHook(events.Common.WindowClosing, func(e) { w.Hide(); e.Cancel() })` (`examples/systray-basic`). Shutdown also
  closes windows, so remove the hook in `app.OnShutdown`.
- `Focus()` calls `SetForegroundWindow` and WebView2 `MoveFocus`; `WM_SETFOCUS` on a window also calls it.
- Positions (`SetPosition`, `Screen.WorkArea`) are in DIP.

## Tray, single instance, notifications, autostart
- Tray: `app.SystemTray.New()`, `SetTooltip`, `SetMenu(application.NewMenu())`, `OnClick`; right click opens the menu by default.
  No icon set → exe icon resource → built-in Wails icon (`systemtray_windows.go`). Menu `OnClick` handlers run on goroutines.
- Single instance: `Options.SingleInstance = &SingleInstanceOptions{UniqueID, OnSecondInstanceLaunch}`. Windows lock = named mutex
  `wails-app-<UniqueID>-sim` (`single_instance_windows.go`); the second instance sends args and working dir to the first and calls
  `os.Exit(ExitCode)` **inside `application.New`**.
- Autostart: `app.Autostart.Enable/Disable/IsEnabled` writes `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` (`autostart_windows.go`).
- Notifications: `pkg/services/notifications` (toast via `git.sr.ht/~jackmordaunt/go-toast/v2` v2.0.3, indirect); used since TASK-053.
  - `notifications.New()` is a process singleton. Its service startup writes `HKCU\Software\Classes\AppUserModelId\<app name>`
    (`CustomActivator` GUID) and `HKCU\Software\Classes\CLSID\<GUID>\LocalServer32` = `"<exe>" %1`, and registers a COM class factory;
    shutdown writes `HKCU\SOFTWARE\<app name>\NotificationCategories` (`notifications_windows.go`).
  - A service whose `ServiceStartup` returns an error aborts `App.Run` (`application.go`, `startupService`), so the shell wraps it
    (`toasts_windows.go`): a failure is logged and the app runs without toasts.
  - Each send saves the app icon as a PNG in `%TEMP%`; when the COM push fails it falls back to PowerShell (temp `.ps1`, `-ExecutionPolicy Bypass`).
  - Toast XML: duration `short`; a scenario only from `InterruptionLevel` (`timeSensitive` = reminder, `critical` = urgent, which can
    break through Focus Assist). The shell sets none, so Windows applies the per-app switch and Focus Assist / Do Not Disturb.
    There is no API to read those settings (`CheckNotificationAuthorization` is always true on Windows).
  - Not checked: clicking a toast after the app exited starts the exe through the CLSID with activation arguments.

## File dialog
- A cancelled `Dialog.OpenFile()` returns `cfd.ErrorCancelled` ("cancelled by user"); `cfd` is internal, so the shell matches the text
  (`schedulepicker_windows.go`). Re-check it on a Wails upgrade.
- In beta.29 a real dialog failure also comes back as "cancelled by user": `hresultToError` compares a `uintptr` with `< 0`, which is never true.

## Frontend runtime
- `/wails/runtime.js` is an ES module served by the app: `import { Window } from "/wails/runtime.js"; Window.Hide()`.
- WebView requests go to `http://wails.localhost/` through WebView2 request interception; no TCP port is opened (`transport_http.go`).
