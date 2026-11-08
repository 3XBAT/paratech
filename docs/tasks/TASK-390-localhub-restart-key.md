# TASK-390: «Перезапустить Hub» and «Выпустить ключ доступа»

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-389
- **PRD sections / AC:** ADR-0036 §5 (Stop, key issuance); PRD §12 (per-tester keys); hard rule 4
- **Агент:** только UI: бета-инструмент; ключ — секрет
- **Branch:** ralph/<date>-task-390

## Goal
After a settings change the owner restarts the Hub with one click, and on a fresh machine gets a Hub access key for
the app without a console.

## Allowed to change
- `paratech/desktop/internal/adapters/hubprocess/*` (`Stop`, `IssueKey`)
- `paratech/desktop/internal/cases/localhub/*` (`Restart`, `IssueAccessKey` bodies)
- `paratech/desktop/internal/input/wails/localhub.go`, `paratech/desktop/frontend/src/bindings/localhub.ts`
- `paratech/desktop/internal/app/localhub_beta.go` (wiring of the desktop `hub/api-key` writer) (+ tests)

## Acceptance criteria
- [ ] `Stop`: PID listening on `127.0.0.1:<port>` (`GetExtendedTcpTable`); acts only if its image path equals our
      hub.exe (case-insensitive, final path); `TerminateProcess`, wait ≤ 5 s for the port to close; another program →
      `port_busy`, untouched.
- [ ] `Restart` = Stop then Ensure; refused while the Hub is not ours.
- [ ] `IssueKey`: runs `hub.exe keys create -label beta-<yyyymmdd-hhmm>` (fixed path, no shell) with only
      `HUB_DATABASE_URL` added, 30 s timeout; stdout read into memory (≤ 4 KiB), the key parsed by the format of
      `hub/internal/input/cli` (check the source), validated with `values.ParseHubAPIKey`, written into the desktop's
      `hub/api-key` slot; never logged, never returned to the binding (result: `issued` | `exists` | `failed`).
- [ ] Offered only when the desktop has no Hub key and the Hub is `running`.
- [ ] Tests: Windows test with the test binary as fake hub.exe for both; a foreign listener is never terminated;
      key text absent from logs and binding results.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` (with and without `-tags beta`), arch test, golangci-lint.

## Notes
Security review required (process termination, key handling).
</content>
</invoke>
