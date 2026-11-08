# TASK-388: Adapters `hubprocess` (spawn detached, probes) and `hubserverconf` (settings file)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-387
- **PRD sections / AC:** ADR-0036 §3, §4, §5; hard rule 4; ARCH-30…34
- **Агент:** не касается
- **Branch:** ralph/<date>-task-388

## Goal
Start `hub.exe` next to the app in the background without a window, with secrets only in its environment, and give
the use case the probe facts.

## Allowed to change
- `paratech/desktop/internal/adapters/hubprocess/*` (new; `*_windows.go` + `*_other.go` stubs returning
  `ErrUnsupported`)
- `paratech/desktop/internal/adapters/hubserverconf/*` (new; std only)
- `paratech/desktop/testenv/*` (fake hub.exe helper if needed) (+ tests)

## Out of scope
- `Stop` and `IssueKey` (TASK-390; return `ErrUnsupported` here). App wiring.

## Acceptance criteria
- [ ] Path = dir of `os.Executable()` + `hub.exe`, regular file, not a reparse point; `exec.Cmd{Path, Args: [path]}`,
      no PATH lookup, no shell, no arguments, `Dir` = `<DataDir>/hub`.
- [ ] Env = parent env minus every `HUB_*` and `LA_*` + `HUB_LISTEN_ADDR=127.0.0.1:<port>`, `HUB_LOG_FILE`,
      `HUB_LOG_LEVEL=info`, the set non-secret settings, and `HUB_ANTHROPIC_API_KEY`, `HUB_DATABASE_URL`,
      `HUB_AGENT_SEAL_KEYS` read from the injected `hubserver` secret reader at spawn time. Missing secret → typed error
      naming the slot.
- [ ] Windows: `HideWindow`, `CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP | CREATE_BREAKAWAY_FROM_JOB`; on
      `ERROR_ACCESS_DENIED` one retry without breakaway and a WARN line. stdout/stderr → `<DataDir>/logs/hub-start.log`
      (truncated per spawn); a `Wait` goroutine records the exit code for `Facts`.
- [ ] `Facts`: `/healthz` and `/readyz` (1 s each, loopback only, no redirects), Docker engine pipe existence, TCP
      connect to the host:port of the database URL (500 ms), hub.exe present, last exit code.
- [ ] No secret in any log line, error text or `hub-start.log` written by us: test with known values grepping logs and
      errors; test that the child's command line has no secret (fake child prints its args and env names).
- [ ] `hubserverconf`: `<DataDir>/hub/hub-server.json`, `schema_version` 1, atomic write (temp + rename), unknown
      fields refused, size cap 4 KiB.
- [ ] Windows test with a fake child (the test binary as hub.exe, as `whispercli` tests do): starts, survives the
      parent's handle close, no console window flag set; `GOOS=linux` build of the stubs.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of both adapters (on Windows too), arch test, golangci-lint.

## Notes
Security review required (subprocess, secrets, paths).
</content>
</invoke>
