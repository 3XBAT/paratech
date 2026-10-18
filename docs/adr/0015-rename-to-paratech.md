# ADR-0015: Rename to «paratech»

- **Status:** Accepted
- **Date:** 2026-10-19
- **Source:** TASK-069; ADR-0007, ADR-0011, ADR-0013, ADR-0014.

## Context
The app was called «Learning Assistant» / «Учебный помощник». The owner renamed it to «paratech», lowercase everywhere.
Data, secrets and the Google sign-in of existing testers must survive the update.

## Decision
**Changes:** the visible name (window titles, tray, pages, sign-in pages, installer, «Программы и компоненты»), the exe
`paratech.exe`, the Start menu shortcut `paratech.lnk`, the install folder of a fresh install
`%LOCALAPPDATA%\Programs\paratech`, the Google calendar name and description, the app icon (orange wave on graphite).

**Kept on purpose** (internal ids, no migration and so no migration risk):
| Id | Value | Why |
|---|---|---|
| Data folder | `%LOCALAPPDATA%\LearningAssistant` (`learning-assistant.db`, `learning-assistant.lock` inside) | Recordings, DB and settings stay; the ADR-0014 secrets profile is "default" only for this path |
| Credential Manager | `paratech/...` | Already named so |
| WebView2 folder | `%APPDATA%\learning-assistant.exe` | Wails derives it from the exe name; the app now sets it (`config.WebviewDataDir`), so localStorage (the theme) stays |
| Single-instance id | `paratech.learning-assistant.desktop` | The installer finds an old running version by its mutex |
| Uninstall key | `HKCU\...\Uninstall\LearningAssistant` | An upgrade reads `InstallLocation` and installs into the old folder |
| Calendar marker | `[paratech-calendar-v1]` | Already neutral |

**Upgrade:** the installer deletes `learning-assistant.exe` in the install folder (stops if it is in use) and
`Learning Assistant.lnk`, then writes the new exe, shortcut, uninstall entry and `paratech:` link. The uninstaller removes
both names. The app re-points `paratech:` at `os.Executable()` at every start (ADR-0013).

**Calendar:** a stored calendar named exactly «Learning Assistant» is renamed with `calendars.patch` (summary and
description) on the next sync: same id, no second calendar. A name the user changed stays. A failed patch is ignored and
retried at the next sync.

## Consequences
- An upgraded install keeps its `Programs\LearningAssistant` folder; only fresh installs use `Programs\paratech`.
- The owner changes «App name» in Google Auth Platform → Branding to «paratech» (`docs/qa/run-alpha.md` §10).
- **Follow-up:** the exe has no icon resource (`.syso`): that needs a build tool (go-winres, rsrc or `wails3`), a new
  dependency. Until then Explorer shows the default exe icon; the window and tray use the embedded `icon.ico`, the
  shortcut and «Программы и компоненты» use `paratech.ico` installed next to the exe.
