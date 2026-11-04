# Playwright over CDP to the Wails WebView2 (E2E exe)

Library: `@playwright/test` **1.64.1** (`playwright-core` 1.64.1, `paratech/e2e/package.json`), `chromium.connectOverCDP("http://127.0.0.1:<port>")`, no browser download.
Target: the E2E build of `paratech.exe` (`-tags production,e2e`, ADR-0035) with `--data-dir <abs> --e2e-dir <abs> --e2e-cdp-port <n> --log-level debug` (+ `--e2e-window WxH`).
Source: throwaway probes of TASK-375 part C on the dev machine, 2026-11-04, run by hand (probe scripts are not in the repo). Page API: https://playwright.dev/docs/api/class-browsertype#browser-type-connect-over-cdp

| Fact | Value |
|---|---|
| Windows | 10 Pro, build 19045.6466 |
| WebView2 runtime | 156.0.4314.58 (registry `...\EdgeUpdate\Clients\{F3017226-...}` value `pv`, read-only; same as `Browser: Edg/156.0.4314.58` of `/json/version`) |
| Wails | v3.0.0-beta.29 (`docs/refs/wails.md`) |
| Node | 22.11.0 |
| Display | 125 % scaling: `devicePixelRatio` 1.25 |

## Проверено 2026-11-04

1. **Versions** — see the table. No Playwright warning or missing method with this WebView2: работает.
2. **CDP attach** — работает. The port answers 0.5-1.0 s after process start; `/json/list` shows both targets 0.2-0.4 s later; the log line `background work started` is at 0.8-1.3 s.
   Targets (type `page`): `http://wails.localhost/` (main) and `http://wails.localhost/popup.html` (popup). **The popup target exists from the start**, while its window is hidden.
   At first the titles are `about:blank` and the DOM can be empty (one run read `body.innerText` = 0 chars right after attach): wait for `#root *` before reading.
   `connectOverCDP`: 1 context (`contexts()[0]`), `pages()` has both pages, connect takes 20-170 ms. `page.evaluate`, `page.locator`, `page.click`, `keyboard.type`, `page.screenshot` на главном окне работают.
   Page titles are `paratech` (main) and `Запись занятия` (popup). The window title `paratech · E2E` is **not** visible to Playwright: it exists only in the OS window title (the main window and the popup window both carry it; the main one is the bigger).
3. **Popup while hidden** — частично. `evaluate` and DOM reads (`getBoundingClientRect`, `innerText`) работают; `document.visibilityState` is `visible`, size 360x300 CSS px (window 450x375 px). **`page.screenshot` не работает**: always a timeout, also with `Page.captureScreenshot` (`fromSurface` true/false), `bringToFront`, `Emulation.setDeviceMetricsOverride`, and after a raw `ShowWindow(SW_SHOWNOACTIVATE)` of the popup window. Cause (Wails source): a hidden-at-creation window has an invisible WebView2 controller; only Wails' own `Show()` calls `chromium.Show()`.
   The screenshot of a popup that the app really showed (`Shell.ShowPopup`) is **not verified**.
4. **Main window screenshot by window state** (a probe button and an input were injected; each state checked: `screenshot`, `rAF`, `click`, `type`):
   - normal and visible: работает, 85 ms, 1263x854 px PNG (inner size x dpr), about 4500 colours, not blank (looked at the file).
   - covered by a topmost window of its own size: работает, same PNG, `visibilityState` `visible`, rAF runs, click and typing работают (Chromium occlusion tracking is not triggered).
   - hidden with a raw `ShowWindow(SW_HIDE)`: работает, same as above (the controller stays visible).
   - **minimized** (`SW_MINIMIZE`): **не работает**. `screenshot` times out, rAF gives no frame in 2 s, `page.click` times out; `evaluate` still returns and `visibilityState` still says `visible`. `ShowWindow(SW_RESTORE)` brings everything back (checked 1.5 s later).
   - **main window closed to the tray** (`WM_CLOSE`, the app hides it with `Window.Hide()`): **не работает**. Screenshot times out; a raw `ShowWindow(SW_SHOW)` does not help (the controller stays invisible). A second launch of the exe with the same arguments (it exits at once, the first instance shows its window) brought the screenshot back.
5. **Loopback** — работает. `netstat -ano -p tcp`: `TCP 127.0.0.1:<port> 0.0.0.0:0 LISTENING <pid>` (pid = a `msedgewebview2.exe` child of the exe, not the exe) plus one `ESTABLISHED` line for the attached client. Nothing on `[::1]` or `0.0.0.0`. Connect to the port through each non-loopback IPv4 address of the machine: one `ECONNREFUSED`, three timeouts (no answer); `::1` refused; `127.0.0.1` and `localhost` connect. Columns are numeric and whitespace separated (state text may be localized; match on `127.0.0.1:<port>` and the last column).
6. **Start and page facts**
   - Log (`<data>/logs/desktop.log`, `time=... level=INFO msg="..."`), in order: `instance lock acquired`, `e2e mode on` (`data_dir`, `e2e_dir`, `cdp_port`, `window`), `credential manager profile` `secrets_profile=isolated`, DEBUG `credential manager profile id` `secrets_profile_id=<id>`, `background work started`. All within about 1.2 s of the process start.
   - Default main window: 1280x900 px (1024x720 DIP at 125 %); `innerWidth` x `innerHeight` = 1010x683 CSS px.
   - `--e2e-window 1280x800`: the window is **1280x800 DIP** (1600x1000 px at 125 %) and `innerWidth` x `innerHeight` = **1266x763**, i.e. the size minus a 14x37 DIP frame; the page never sees exactly WxH.
   - `navigator.clipboard.writeText` is a function; `isSecureContext` true; `permissions.query({name:"clipboard-write"})` gives `granted`. The call itself was **not made** (the system clipboard held an image; it could not be restored).
   - The E2E exe ran next to the owner's running beta `paratech.exe` without a conflict (own Wails instance id). So never stop or find the E2E app by image name: use the pid you started.
7. **Stability** — работает. Attach, `browser.close()`, attach again, `browser.close()`: the exe and the port stay alive after each close (`close()` only disconnects with `connectOverCDP`). Hard stop by `taskkill /PID <pid> /T /F` removed the exe and all its `msedgewebview2.exe` children. The probes left no Credential Manager entry (`cmdkey /list` had no `paratech-<id>/` target).

## Что из этого следует для раннера
- Find pages by URL from `context.pages()` (`/popup.html` popup, the other main); wait for `#root *` before any read. Poll the port every ~50 ms, give it 30 s.
- Screenshots of the main window work when it is visible, covered or SW_HIDE-hidden; **do not rely on them minimized or closed to the tray**. Before a screenshot or click, check with `EnumWindows` (by the exe pid and the title ending ` · E2E`) that the main window is not iconic and is visible; if iconic, `ShowWindow(SW_RESTORE)` and wait for a frame. A tray-hidden window is restored only by a second launch with the same arguments; better never to close the main window in a scenario.
- Popup: DOM checks (layout invariants) work while hidden; take its screenshot only after the app showed it for real, with a short timeout, and report «не проверено» when it fails.
- Use `browser.close()` as disconnect; kill the app only by the pid and `/T`. Never kill by `paratech.exe` name.
- Loopback check: parse `netstat -ano -p tcp`, require the `LISTENING` line of the port to be `127.0.0.1:<port>` only.
- Size checks: `--e2e-window` is in DIP; expect `inner` = size - (14, 37) at 125 %, or read `innerWidth` and do not compare with WxH.
- Read `secrets_profile_id` from the debug log; delete only `paratech-<id>/` targets.

## Не проверено
- A real popup shown by the app (screenshot, click through `Начать запись`).
- `navigator.clipboard.writeText` result (the owner's clipboard was not touched) and the E2E recorder's `opened.jsonl` for it.
- Other DPI scales (frame size and dpr) and several monitors; other WebView2 versions.
- Native dialogs and the tray menu (not reachable from CDP); `SW_MAXIMIZE`; a window dragged off-screen.
- The E2E exe in `dist/e2e` was already there (built 2026-11-04 02:42, has all `--e2e-*` flags) and was not rebuilt for the probe.
