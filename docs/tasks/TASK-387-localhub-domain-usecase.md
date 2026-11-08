# TASK-387: Local Hub: domain values, diagnosis rule and use case `cases/localhub` (contracts first)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** none
- **PRD sections / AC:** ADR-0036 §2, §3, §4, §7
- **Агент:** только UI: вводится секрет; бета-инструмент (TASK-391 adds the parity row)
- **Branch:** ralph/<date>-task-387

## Goal
The rules of «should the beta start a Hub and what does the owner see» exist as pure code with the ports the
adapters (TASK-388, TASK-390) and the wiring (TASK-389) build on.

## Allowed to change
- `paratech/desktop/internal/domain/localhub/doc.go`, `domain/localhub/values/*`,
  `domain/localhub/services/diagnosis/*` (new)
- `paratech/desktop/internal/cases/localhub/*` (new: `service.go`, `ports.go`, `types.go`, `errors.go`, tests on fakes)
- `docs/architecture.md` §12 (one row: context `localhub`, beta only, ADR-0036)

## Out of scope
- Adapters, app wiring, bindings, UI. `Restart` and `IssueAccessKey` bodies (TASK-390); their ports are declared here.

## Acceptance criteria
- [ ] `values.Target`: parsed from «Адрес сервера» — `http`, host `127.0.0.1` or `localhost`, explicit port 1..65535,
      no path/query/user info; else `NotLocal`; listen address is always `127.0.0.1:<port>`.
- [ ] `values.ServerSettings`: base URL (https, or http to loopback; no user info, query, fragment), auth
      (`x-api-key` | `bearer`), quota limit (0 = default, else > 0); invalid → domain error.
- [ ] `values.Status` (`remote`, `running`, `starting`, `degraded`, `not_running`) and `Reason` (closed list of
      ADR-0036 §7); `diagnosis.Diagnose(facts)` with the precondition order of §4; table test of every row.
- [ ] `cases/localhub.Service`: `Ensure` (probe → diagnose → spawn only when every precondition holds → wait for
      readiness within 20 s through `Clock`), `Status`, `Recheck`, `SaveSettings`, `SetSecret(name)`/`SecretSet(name)`
      for `provider-key`, `database-url`; seal keys `Generate` when missing before the first spawn. Concurrent `Ensure`
      calls collapse into one (no second spawn). No secret value ever passes through the use case (ports take names).
- [ ] Ports in `ports.go` exactly as ADR-0036 §2 (`HubProbe`, `HubLauncher` with `Start`, `Stop`, `IssueKey`,
      `SecretSlots`, `SettingsFile`, `HubURL`, `Clock`).
- [ ] Tests on fakes: every reason; running Hub not spawned again; Docker off → no spawn; spawn then exit 2 →
      `config_error`; recheck after Docker on → spawn; remote URL → nothing.
- [ ] `go vet`, `go test -race` of the new packages, arch test, golangci-lint v2.14.0.
</content>
</invoke>
