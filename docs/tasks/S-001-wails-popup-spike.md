# S-001: Wails v3 popup / tray spike

- **Agent:** go-core (+ user verifies on Windows)
- **Label:** spike
- **Depends on:** T-001
- **PRD sections / AC:** §6.1, AC-01, AC-02
- **Branch:** task/S-001-wails-popup

## Goal
Answer: can Wails v3 show a small non-modal popup that does not steal focus from another app (e.g. Zoom), plus a tray icon and a native notification? Also check single-instance and autostart feasibility.

## Allowed to change
- `desktop/spikes/wails-popup/` (throwaway), `docs/adr/` (result)

## Acceptance criteria
- [ ] Minimal Wails v3 app with a main window, tray icon, and a button/timer that opens a frameless always-on-top popup
- [ ] Documented result (works / workaround / not possible) for: no focus steal, click-through to other apps, multi-monitor placement, notification
- [ ] Pin the Wails v3 version used; note API gotchas in `docs/refs/wails.md`
- [ ] User confirms behavior on a real Windows machine with Zoom or similar in the foreground
