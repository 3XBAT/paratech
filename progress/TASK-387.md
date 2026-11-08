# TASK-387: Local Hub, domain values, diagnosis rule and use case `cases/localhub`

## Done
- `domain/localhub/values`: `Target` (http, `127.0.0.1`/`localhost`, explicit port 1..65535 without leading zeros, path empty or `/`, no query/fragment/user info; else `ErrNotLocal`; `ListenAddr()` always `127.0.0.1:<port>`), `ServerSettings` (mirrors the Hub's own base-URL validation; empty URL/auth/0 quota = Hub default), `SecretName` (closed list; typed input only `provider-key`, `database-url`), `Facts` (flags only, no secret values), `Status`, `Reason` (8 codes of ADR §7), `NextStep` (stable code per reason; Russian text is the UI task's), `Diagnosis`.
- `domain/localhub/services/diagnosis.Diagnose(facts)`; table test of every row.
- `cases/localhub.Service`: `Ensure` (automatic), `Recheck` (explicit), `Status`, `Settings`, `SaveSettings`, `SetSecret`/`SecretSet`; ports in `ports.go` as ADR §2. Single flight (concurrent calls: one start), flight detached from the caller's ctx and bounded (30 s). Seal keys generated before the first spawn. No Docker or compose start anywhere; `Restart`/`IssueAccessKey` bodies left to TASK-390 (ports `Stop`/`IssueKey` declared).
- `docs/architecture.md` §12: row `localhub`.
- Owed from TASK-385 (arch test, `desktop/tests/arch_test.go`): a file without a `//go:build` that requires `beta` must not import `cases/localhub`, `adapters/hubprocess` or `adapters/hubserverconf` (works while the adapters do not exist; fixtures for violations and clean cases). TASK-389 does not need to add it again.

## Decisions to confirm
1. `Diagnose` returns `values.Diagnosis` (status + reason + exit code), not a bare `Status` as ADR §2 wrote; ADR §2/§4 wording should be amended (also the checks after the five preconditions: `port_busy`, our listener that does not answer = `starting`, then exit reasons).
2. Exit handling (reviewer high, dead end): the automatic `Ensure` never respawns over a recorded Hub exit (no respawn loop). `Recheck`, a successful `SaveSettings` and a successful `SetSecret` acknowledge the exit and allow ONE new start; an acknowledgement covers only an exit that is already recorded. Contract for TASK-388/390: the recorded last exit belongs to the latest spawn, and a successful `Start` forgets the previous one. The wiring (TASK-389) must call `Ensure` for the 60 s recheck and `Recheck` for the button.
3. Start failures have no reason of their own (ADR list is closed): failed `Start`/seal-key generation = `exited` with `ExitNotStarted` (-1), unreadable settings = `config_error` with exit 0. TASK-391 should render -1 as «не удалось запустить», not «код -1»; or the architect adds a `start_failed` reason.
4. A 20 s silence after a successful start shows `starting`.
5. Secret flags in `Facts` are filled by the Service from `SecretSlots.IsSet`; the probe may leave them false. `SetSecret(name, value)` follows the ADR (value goes straight to the port, trimmed, never kept/logged; the task text «ports take names» is looser than ADR §2).
6. `hubserverconf` is in the beta-only arch rule beyond ADR §1's literal list (settings-file adapter used only by the beta wiring).
7. Open owner questions О1 (compose up) and О2 (detach) are untouched; nothing in the code decides them.

## Follow-ups
- Gap (reviewer M1, accepted): `SaveSettings`/`SetSecret` do not probe, so one called while the Hub runs and followed by a crash before the next automatic `Ensure` still covers that crash (one respawn at most). Fix would be a probe on save.
- L1: a `Recheck` that joins an already running automatic check may return that check's result (not a new start); the acknowledgement stays pending, so the next tick starts the Hub.
- L2: a failed `Start` does not spend the acknowledgement, so every automatic `Ensure` retries (no process loop).
- Remote `Status.URL` is shown without user info.
- TASK-388/390 reviews must check the adapter honours the exit contract above.

## Changed existing tests
`desktop/tests/arch_test.go`: header comment, new subtest `beta-only packages` in `TestRealTree`, two new rows in `TestCheckersDetectViolations`. No assertion changed or removed.

## Review
reviewer (opus): changes requested (high: no way out of `exited`/`config_error`; medium: arch-test scope, cancelled single-flight leader, start-failure semantics; lows) -> fixed; re-review approve with M1 (an ack saved up while no exit is recorded) and M2 (hung adapter call blocks the flight) -> both fixed in 836b1620 (no further re-review: small, tested). security-auditor (opus): no findings.

## Security
Pure code, no process/file/network. Target parsing is loopback-only and the listen address is built from a constant and an int port; settings values reject control characters; secret values are not in Facts/Status/errors/logs; the beta-only import gate was checked against `go/build` constraint parsing.
