# TASK-407: Local Hub, start of the database container (domain + use case)

## Done
- `domain/localhub/values`: `Facts.DatabaseStartable` (set by the probe, TASK-408) and `Facts.DatabaseStartFailed` (set only by the use case); `StatusStartingDatabase`; `ReasonDatabaseStartFailed` (next step `open_logs`, in `Reasons()`); `Diagnosis.CanStartDatabase()`, constructors `DatabaseDown(hubAnswers, startable)` and `StartingDatabase()`.
- `diagnosis.Diagnose`: engine on + DB port silent: `DatabaseStartFailed` -> `database_start_failed`; else `database_down`, `CanStartDatabase` only if startable; same for degraded (status stays degraded). Engine off -> `docker_off` always wins; not startable -> old manual `database_down`; earlier preconditions still first. Table test extended (11 rows + column).
- `cases/localhub`: port `DatabaseStarter.StartDatabase(ctx)`, `Deps.Database` (required by `New`). `Ensure` on `CanStartDatabase`: state `starting_database`, `StartDatabase` (150 s ctx deadline), poll the DB port every 1 s up to 30 s through the `Clock`, then (for a degraded Hub) wait up to 20 s for `/readyz` without restarting the Hub, then re-diagnose and the normal Hub start. Inside the existing single flight; `flightTimeout` is now `150+30+2*20+10` s.
- Failure memory (`dbFailed`) cleared by `Recheck`, successful `SetSecret(database-url)` (not `provider-key`), or the engine seen off and on again. A generation counter keeps a clear that lands during a running start from being overwritten by that start's failure.
- Tests on fakes (`database_test.go`, no real sleeps): docker off, startable then Hub start, polling, 30 s silence = failure, failure not retried automatically and retried after Recheck / new URL / engine off-on, degraded without Hub restart, readyz lag (ready after polls / never ready), not startable, concurrent single flight, flight timeout not counted as a failure.

## Decisions to confirm
1. A DB port still silent 30 s after a successful `StartDatabase` counts as `database_start_failed` (remembered; WARN log, no error returned). ADR §8 does not say it outright.
2. A started container does not acknowledge a recorded Hub exit: if the Hub exited because the DB was down, the automatic path stops at `exited` after the container start until the owner acts (keeps the no-respawn guarantee). Alternative: one automatic Hub start after the app itself started the DB.
3. «Once» in ADR §8 is implemented as once per remembered failure: a container that starts and dies again later is started again by the next 60 s `Ensure` (idempotent compose start, paced by the recheck).
4. The 150 s limit is a context deadline, not the Clock.

## Follow-ups
- TASK-408 (adapter): `StartDatabase`, probe sets `DatabaseStartable`; security review must cover: fixed absolute paths for docker.exe and the compose file re-checked at run time, not writable by other users; compose output never in errors/logs; loopback check redone inside `StartDatabase`; no database URL with password in logs.
- TASK-389 wiring must pass `Deps.Database`.
- UI (TASK-391): Russian texts for `starting_database` («Запускается база данных…») and `database_start_failed`; treat `starting_database` as transient (re-poll), do not give `Ensure` a short context (flight up to ~220 s).
- Low (reviewer): "not ready after its database started" WARN can fire for other causes than the deadline; the status after a never-ready Hub reads `database_down` / start_database although the port answers (wrong URL likely): UX follow-up; a Recheck joining a running flight returns that flight's result.

## Changed existing tests
`values_test.go` `TestEveryReasonHasANextStep`: 9 reasons / 8 distinct next steps (was 8/8), because `database_start_failed` shares `open_logs` with `exited` as the task requires; added a check of its next step. No assertion removed.

## Review
reviewer (opus): approve, 2 mediums (lost update of the failure memory while a start runs; misleading status while `/readyz` lags the port) -> fixed in a9cff950; re-review approve (lows above). security-auditor (opus): no findings (code not reachable yet; notes for TASK-408 above).

## Security
Pure use case/domain, no process, file or network code. `StartDatabase` takes no arguments, so no user or LLM value reaches a command. Docker Desktop is never started. Secret values are not logged. Failure memory is bounded (one extra attempt per owner action).
