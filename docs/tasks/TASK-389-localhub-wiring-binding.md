# TASK-389: Wire the local Hub under tag `beta` and bind it to the UI

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-385, TASK-386, TASK-387, TASK-388
- **PRD sections / AC:** ADR-0036 §1, §2, §4, §7; ADR-0009 (start order), ARCH-50, ARCH-64
- **Агент:** только UI: вводится секрет; бета-инструмент
- **Branch:** ralph/<date>-task-389

## Goal
In the beta build the app ensures its Hub at start and every minute while it is down; the page can read the state,
save settings and secrets and open the logs folder. The student build links none of it.

## Allowed to change
- `paratech/desktop/internal/app/localhub_beta.go`, `localhub_nobeta.go` (new), `app.go`/`env.go` (only the hook call)
- `paratech/desktop/internal/input/wails/localhub.go` (new `LocalHubService`), `shell_localhub_beta.go`,
  `shell_localhub_nobeta.go` (new; registration only)
- `paratech/desktop/frontend/src/bindings/localhub.ts` (new; `available()` false when the binding is missing)
- `paratech/desktop/tests/arch_test.go` (rule: only files with `//go:build beta` import `cases/localhub`,
  `adapters/hubprocess`, `adapters/hubserverconf`) (+ tests)

## Out of scope
- UI pages (TASK-391); restart and key issuance (TASK-390).

## Acceptance criteria
- [ ] UI mode only, after `start` (ADR-0009), in the background; never in headless, recorder or E2E mode. `Close`
      stops the recheck loop and never stops the Hub.
- [ ] Recheck every 60 s while not `running`/`remote`; `LocalHubService.Recheck()` for «Проверить снова».
- [ ] Binding: `Status() → {state, reason, port, exitCode}`, `Settings()`/`SaveSettings(...)`,
      `SetProviderKey(v)`, `SetDatabaseURL(v)`, `SecretsSet() → {providerKey, databaseURL}` (never values),
      `OpenLogsFolder()` (fixed `<DataDir>/logs` through the existing explorer opener). Problems as closed codes.
- [ ] `hubserver` secret store via `wincred` (names of ADR-0036 §3, ADR-0014 profile); seal key generation through
      `random`.
- [ ] Non-beta build: `go tool nm` check of TASK-385 passes with the packages present in the module; arch rule passes.
- [ ] Tests: tagged wiring (`go test -tags beta ./desktop/internal/app/...`) with fakes; binding tests; frontend
      binding parse tests.
- [ ] `go vet` and `go test -race` with and without `-tags beta`, `GOOS=windows go vet`, golangci-lint,
      `npm --prefix paratech/desktop/frontend run test`.

## Notes
Security review required (secret entry path, opener path).
</content>
</invoke>
