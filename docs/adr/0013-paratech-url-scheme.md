# ADR-0013: The paratech: URL scheme

- **Status:** Accepted
- **Date:** 2026-10-16
- **Source:** TASK-066 (the Google sign-in page and its «Открыть Paratech» button); ADR-0007, ADR-0009, ADR-0011.

## Context
After the Google sign-in the browser shows the loopback answer page. The owner wants a button there that brings the app back.
A web page can only do that through a URL scheme registered for the app.

## Decision
- **Per user, no admin:** `HKCU\Software\Classes\paratech`: default `URL:Paratech`, `URL Protocol` = `""`,
  `shell\open\command` default = `"<absolute exe path>" "%1"` (path and link both quoted). Never HKLM.
- **Who writes it:** the installer (TASK-054) on install, deleting the key on uninstall; the app itself at every start of
  the instance with the UI (`adapters/urlscheme`, called from `app.openCore`): if a value is missing or differs, `shell` names
  another default verb or the command has a `DelegateExecute`, it deletes `shell\open\command`, `shell\open`, `shell` and
  rewrites all three. A failure is logged, never fatal. An exe path with `"`, `%` or a control character is not registered.
- **The link is ignored.** A launch with an argument starting with `paratech:` (any case) parses no flags at all
  (`config.Load`). The second instance exits through Wails' single-instance check before opening anything; the first one
  ignores the second launch's data and only shows and focuses the main window. Not running: the app starts normally.
- The only link the app uses is `paratech://open`; no path, query or action is ever read.
- After a successful sign-in the main window also comes forward on its own (`GoogleService`).
- The success page opens `paratech://open` by itself via `<meta http-equiv="refresh">`, with a small fallback link (TASK-073); the denied and failed pages keep the button.

## Consequences
- Any web page or local program can open `paratech:` links; the worst outcome is the main window coming forward.
- Dropping all arguments covers a link with a `"` that splits into several arguments under the Windows command-line rules.
- A copied or `go run` binary takes the registration over at its start; the installed app takes it back at its next start.
- Windows may refuse a background window the foreground; the launching instance calls `AllowSetForegroundWindow(ASFW_ANY)`
  before the Wails check so the first instance may come forward. Live behaviour is checked in `docs/qa/windows-checklist.md`.
