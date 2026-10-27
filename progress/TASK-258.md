# TASK-258: weblink.OpenCallLink

## Done
- `Opener.OpenCallLink(ctx, values.CallLink)` in `adapters/weblink/calllink.go`: re-parses `String()` with `ParseCallLink`, requires canonical equality and printable ASCII without `"`, then always opens through the temporary `.url` (`startShortcut`, the `OpenAuthPage` path). The URL never reaches a command line. Refusal: `ErrRefused`; non-Windows: `ErrUnsupported`; cancelled context: nothing written or started.
- `OpenLink`, `OpenAuthPage` unchanged in behaviour; `startShortcut` now calls the test seam `startShortcutFile` (default `start`).
- Tests: allowed links (Zoom `?pwd=`, Teams `%3a`, http, 2048 bytes), 30-case refusal table, exact `.url` content, launcher gets only the temp path, cancelled context. Launcher faked, TMP redirected; no browser opened.
- ADR-0032 Open 1-4 untouched; no host list.

## Decisions to confirm
- None.

## Follow-ups
- Security (auditor, conditional, Low-Medium, ~4/10): `%NAME%` with hex-prefixed NAME (e.g. `?x=%DB_PASSWORD%20`) passes the gate; if the Internet Shortcut handler expands env vars in `URL=`, a value could leak to the link's own host on click. Unconfirmed; refusing it would also refuse real Teams `%22Tid%22` links, so not done. Verify on a Windows machine before deciding.
- The `calllink_test.go` "environment variable" case (`%USERPROFILE%`) is refused only because `U`,`S` are not hex; add a hex-prefixed case if hardening lands.
- Nit: `ErrRefused` message says "plain https page"; could be generalized.

## Changed existing tests
None.

## Security
Reviewer: approve. Security-auditor: no confirmed findings; one conditional note above.
