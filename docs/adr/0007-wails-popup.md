# ADR-0007: Wails v3 shell, tray and recording popup

- **Status:** Accepted (behavior on a real lesson to be confirmed in TASK-020)
- **Source:** TASK-019, spike S-001, PRD §6.1, AC-01, AC-02; API notes in `docs/refs/wails.md`

## Decision
- Pin `github.com/wailsapp/wails/v3` **v3.0.0-beta.29**. The Windows backend is pure Go, so the desktop binary stays `CGO_ENABLED=0`.
- The UI shell lives in `internal/input/wails`; Wails is imported only from `*_windows.go`. Elsewhere `Shell.Run` returns `ErrUnsupported`.
- `app.Run` runs the shell on the main goroutine (Wails locks the main OS thread) until the context is cancelled or the user picks Quit
  in the tray. `--headless` / `LA_HEADLESS` runs the core without UI (tests, non-Windows). The Wails signal handler is off; `main` owns signals.
- Windows: a main window (close = hide), a tray icon (Open, Show recording popup, Quit) and a popup window created hidden.

## Result (checked on Windows 10 22H2, WebView2 155, via scripts; not with Zoom)
| Goal | Result |
|---|---|
| Popup without focus steal | Works with a workaround. Popup: frameless, always on top, off the taskbar, fixed size, bottom-right of the work area, `WS_EX_NOACTIVATE`. Wails `Show()` activates it anyway, so `ShowPopup` first calls `ShowWindow(SW_SHOWNOACTIVATE)`; then the foreground window stayed unchanged (measured in a scratch program with the same window options and show sequence). Clicking the popup may activate it. |
| Tray | Icon appears (Wails default until an app icon exists). Menu and click handlers are wired but were not clicked by the scripts. |
| Single instance | Works: Wails `SingleInstance` (named mutex). A second launch exits with code 0 within ~70 ms; the first one gets a callback that shows its main window (not observed visually). |
| Graceful stop | Works: Ctrl+Break (Go maps it to `os.Interrupt` like Ctrl+C) cancels the context, the shell quits and `main` logs "gracefully stopped". |
| Notifications | Wails `services/notifications` (toast); not used: removed by TASK-065, owner decision; see ADR-0009. |
| Autostart | Feasible: `app.Autostart.Enable` (HKCU `Run` key). Not enabled: it must be a user setting. |

## Consequences
- Creating a window activates it once, so at startup the hidden popup may briefly take the foreground before the main window shows.
  Harmless now (the main window is shown at start); a start minimized to tray (autostart) must create the popup lazily or restore the foreground.
- The single-instance check runs in `Shell.Run`; the core (SQLite) is opened by the Shell option `OpenCore` after `application.New`,
  so a second instance exits before the DB opens. Since TASK-056 (ADR-0009) headless mode opens and starts the core too, and
  `openCore` takes a data-dir file lock before the DB in every mode; background work starts from the `Options.Started` hook
  (`ApplicationStarted`), when the shell is ready.
- Default data dir is `%LOCALAPPDATA%\LearningAssistant`, DB file `<DataDir>/learning-assistant.db` (to be confirmed by the owner); both kept by the rename to «paratech» (ADR-0015).
- Release builds must use `-tags production` (see `docs/refs/wails.md`).
- Toasts of TASK-053: removed by TASK-065, owner decision; see ADR-0009.
- TASK-022: `Shell` also implements the `recordingprompt.Popup` port (`ShowPopup`/`HidePopup`); `ShowPopup` emits the
  Wails event `recordingprompt:shown` and the page reads `PromptService.Current`. Tray "Show recording popup" asks the
  core for an ad-hoc prompt. The popup page becomes the React app's second entry (TASK-022 frontend); size 360x180, 360x300 since TASK-059 (source picker).
  The `Popup` port takes no ctx: Wails window commands cannot be cancelled; the shutdown bounds them (`endLoops`).
- TASK-076 (owner decision 2026-10-18): the lesson popup appears at the lesson start (`scheduler.DefaultLead` = 0), still up to
  20 s late (`lesson.StartLateGrace`); no separate Windows notification. It has only «Начать запись» and «Не записывать»:
  the snooze («Через 5 минут») is removed from the prompt aggregate, the use case and the binding. Prompts live in memory
  only, so no stored state had to migrate.

## Check on Windows by a human (TASK-020)
1. Start a Zoom/Telemost call and click into its chat. Open the popup from the tray menu, then switch back to the call; better, let a test
   build show it on a timer. Typing in the call must continue; the call window must stay in front of the main window.
2. The popup stays above a fullscreen or maximized call window, sits next to the tray on the primary monitor, and Dismiss hides it.
3. With two monitors (different scale): popup placement and size are correct.
4. Closing the main window keeps the tray icon; tray "Open" brings it back; tray "Quit" ends the process.
5. A second launch brings the running app to the front and exits.
