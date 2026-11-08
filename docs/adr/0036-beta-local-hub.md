# ADR-0036: The beta starts its local Hub

- **Status:** Proposed (owner request 2026-11-08; proposal agreed with the owner, open items below)
- **Date:** 2026-11-08
- **Source:** owner 2026-11-08: no more console, path, env vars and `go run` to start the Hub; logs in a file.
  ADR-0003, 0009 (lifecycle, log file, connections), 0014 (secrets profile), 0035 (build tag `e2e`), PRD §12, §4.3.

## Context
The owner's beta (`build-alpha.ps1 -OutDir paratech\dist\beta`) talks to a Hub the owner starts by hand: Docker for
Postgres, a console, `HUB_DATABASE_URL`, `HUB_ANTHROPIC_API_KEY`, `HUB_AGENT_SEAL_KEYS`, `go run ./hub/cmd/hub`; the
Hub logs to stderr only. Facts checked for this ADR:
- `hub serve` listens first, then opens and migrates Postgres; a database it cannot reach ends the process (exit 1).
  A config error exits 2 from `config.MustLoad`, before any logger. `/healthz` answers `ok` while serving, `/readyz`
  pings the database (503 `unavailable`). The default listen address `:8080` binds every interface.
- Secrets of the Hub are env-only by design (`DatabaseURL`, `AnthropicAPIKey`, `AgentSealKeys` are never printed).
- The desktop already runs child processes by a fixed path next to the exe (`audiocapture` recorder, `whispercli`),
  keeps secrets in `wincred` per data-dir profile (ADR-0014) and writes `<DataDir>/logs/desktop.log` with size rotation
  (`adapters/logfile`, 5 MiB, two old files).
- The student product uses the cloud Hub: **its build must never contain or start a Hub.**

## Decision

### 1. Gating: build tag `beta`
- The feature exists only in a build with tag **`beta`**: `build-alpha.ps1 -Beta` builds `-tags production,beta` into
  `paratech\dist\beta` (default out dir) and also `hub.exe` next to `paratech.exe`. `-Beta` and `-E2E` exclude each
  other (the E2E runner starts its own Hub, ADR-0035 §4). Without `-Beta` the script keeps checking that the exe reports
  exactly `-tags=production`; `build-release.ps1` never passes `-Beta`.
- A config flag was rejected: the student exe would carry the spawn code and a way to switch it on.
- Domain, use case and adapters are ordinary packages (built and tested by CI in every build). Only the wiring is
  tagged: `internal/app/localhub_beta.go` (`//go:build beta`) and `localhub_nobeta.go` (`//go:build !beta`, returns
  nil), and the registration of the binding in `input/wails` likewise (`shell_localhub_beta.go` / `_nobeta.go`). So
  the linker drops every package of the feature from the student exe. Checks: the build script runs `go tool nm` on a
  non-beta exe and fails on any symbol of `cases/localhub` or `adapters/hubprocess`; the arch test fails when a file
  without `//go:build beta` imports them; CI vets and tests `-tags beta` as it does `-tags e2e`.

### 2. Placement (desktop)
| Concept | Where |
|---|---|
| New bounded context `localhub` (beta tooling, no aggregate) | `domain/localhub/values`: `Target` (loopback port parsed from the desktop's «Адрес сервера»: `http`, host `127.0.0.1` or `localhost`, explicit port, no path; anything else = not local), `ServerSettings` (non-secret Hub settings, validated), `Status` + `Reason` (closed enums) |
| Diagnosis rule | `domain/localhub/services/diagnosis`: pure `Diagnose(facts) Status` from probe facts (secrets present, hub.exe present, healthz, readyz, Docker engine, database port, last exit code) |
| Use case | `cases/localhub`: `Ensure(ctx)` (probe; if nothing answers and every precondition holds, spawn and wait for readiness), `Status(ctx)`, `Recheck(ctx)`, `SaveSettings`, `SetSecret(name, value)` / `SecretSet(name) bool` (write-only), `Restart(ctx)`, `IssueAccessKey(ctx)`; ports in its `ports.go` |
| Spawn, probes, stop, key issuance | `adapters/hubprocess` (`//go:build windows` + `_other.go` stub returning `ErrUnsupported`, ARCH-33) |
| Non-secret settings file | `adapters/hubserverconf`: `<DataDir>/hub/hub-server.json` |
| Secrets | existing `adapters/wincred`, new store of service `hubserver` (names below) |
| Binding | `input/wails.LocalHubService` (ARCH-42 narrow interface on `cases/localhub`) |
| Wiring | `internal/app/localhub_beta.go` only (ARCH-50) |

Ports of `cases/localhub` (types cross as domain values or primitives, ARCH-26):
`HubProbe.Facts(ctx, Target) (values.Facts, error)`, `HubLauncher.Start(ctx, Target, values.ServerSettings) error`,
`HubLauncher.Stop(ctx, Target) error`, `HubLauncher.IssueKey(ctx) error`, `SecretSlots.IsSet(ctx, name) (bool, error)`,
`SecretSlots.Set(ctx, name, value) error`, `SecretSlots.Generate(ctx, name) error` (seal keys), `SettingsFile.Load/Save`,
`HubURL.Current(ctx) (string, error)` (bridge over the desktop settings), `Clock`. **The use case never reads a secret
value**: the launcher and the probe read them from the `hubserver` store they are given in `app` (a structural
interface of the adapter, satisfied by `wincred`; no adapter imports another, ARCH-30).

### 3. Where the Hub settings live
| Setting | Where | Entered |
|---|---|---|
| Provider key (`HUB_ANTHROPIC_API_KEY`) | Credential Manager `paratech/hubserver/provider-key` | once, «Подключения → Hub на этом компьютере (бета)», write-only («задан / не задан») |
| Database URL (`HUB_DATABASE_URL`, holds the DB password) | `paratech/hubserver/database-url` | same block, write-only; the field's placeholder shows the `hub/docker-compose.yml` URL with port 55432 |
| Seal keys (`HUB_AGENT_SEAL_KEYS`) | `paratech/hubserver/seal-keys` | never: generated once (`1:` + 32 random bytes hex, `crypto/rand` through the adapter) when missing |
| Base URL, auth mode, quota limit | `<DataDir>/hub/hub-server.json` (`schema_version: 1`, `anthropic_base_url`, `anthropic_auth`, `quota_limit`; empty/0 = the Hub default) | same block |
| Listen address | derived: `127.0.0.1:<port of «Адрес сервера»>`; never all interfaces | — |
| Log file | derived: `<DataDir>/logs/hub.log` | — |
Names follow ADR-0014: another data dir uses `paratech-<profile>/hubserver/...`. Hard rule 4 holds: no secret in a
file, an env default, a command line or a log. Everything else stays at the Hub's defaults (README table).

### 4. Start, readiness, no duplicates
- Runs in UI mode only (not headless, not `--recorder`, not E2E), after `start` (ADR-0009) in the background; never
  delays the window.
- `Ensure`: no local `Target` → status `remote` (nothing to do). `GET /healthz` (1 s timeout) `ok` → `running` (whoever
  started it, also a Hub the owner runs by hand); then `/readyz` decides `running` or `degraded` (database). Otherwise
  check preconditions in this order and stop at the first missing one: `hub.exe` next to the exe; provider key; database
  URL; Docker engine (`\\.\pipe\docker_engine` exists, existence only, no connection; to verify); the database port of
  the URL accepts TCP (500 ms). Then spawn, poll `/healthz` every 500 ms up to 20 s, then `/readyz`.
- **The listening port is the cross-process lock**: a second `hub.exe` fails `listen` and exits; the desktop's own
  instance lock (ADR-0009) keeps one desktop per data dir. No extra lock file.
- **Recheck** every 60 s while the status is not `running` or `remote`, and on «Проверить снова»: so a Hub comes up by
  itself after the owner starts Docker Desktop. A Hub that dies while the app runs is found by the same recheck.
- Closing the app leaves the Hub running (the child is detached; the desktop never stops it at `Close`).
- **The app never starts Docker Desktop or a container** (owner rule). It reports it.

### 5. Spawn (security-relevant, `adapters/hubprocess`)
- Path: `filepath.Join(filepath.Dir(os.Executable()), "hub.exe")`, resolved once; must be a regular file, not a reparse
  point (as the speech component). `exec.Cmd{Path: abs, Args: []string{abs}}`: no PATH lookup, no shell, no
  arguments, `Dir` = `<DataDir>/hub`.
- Env: the parent's environment minus every `HUB_*` and `LA_*` variable, plus `HUB_LISTEN_ADDR`, `HUB_LOG_FILE`,
  `HUB_LOG_LEVEL=info`, the non-secret settings that are set, and the three secrets read from Credential Manager right
  before the start. Secrets never on the command line (visible in the process list), never in a log line or an error
  (errors name the variable, not the value). Accepted: a same-user process can read another process's environment;
  that is the Hub's own design and the same trust boundary as Credential Manager.
- Windows: `SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP |
  CREATE_BREAKAWAY_FROM_JOB}`; if the breakaway is refused (`ERROR_ACCESS_DENIED`, the desktop runs in a job without
  breakaway rights), retry once without it and log that the Hub may end with the app (to verify on the owner's machine).
  The Hub is not put into a job object of ours.
- Stdout and stderr go to `<DataDir>/logs/hub-start.log` (truncated at every spawn): it catches what the Hub writes
  before its logger (config errors, exit 2) and panics. A `Wait` goroutine records the exit code for diagnostics; the
  desktop's handle closes with the desktop.
- **Stop** (only for «Перезапустить Hub»): find the PID that listens on `127.0.0.1:<port>` (`GetExtendedTcpTable`),
  accept it only if its image (`QueryFullProcessImageName`) is exactly our `hub.exe`, then `TerminateProcess`; any
  other listener is never touched (status `port_busy`). A hard stop is safe for the Hub: reservations expire by TTL,
  every write is transactional (ADR-0008).
- **Key issuance** («Выпустить ключ доступа», when the desktop has no Hub key): `hub.exe keys create -label
  beta-<yyyymmdd-hhmm>` with only `HUB_DATABASE_URL` added; stdout is read into memory, the key is parsed and written
  straight into the desktop's `hub/api-key` slot; never logged, never returned to the UI.

### 6. Hub log file (`hub`)
- New config `HUB_LOG_FILE` / `-log-file` (absolute path, not a secret). When set, the Hub logs only to that file,
  size-rotated by a new `hub/internal/adapters/logfile` (5 MiB, `hub.1.log`, `hub.2.log`, as the desktop's); the file is
  attached **after** the listener is bound (the port is the lock, so two Hubs never rotate one file). Before that and
  without the variable: stderr, as today. `main` stays linear (ADR-0003); the sink is opened by `internal/app`.
- `hub/docker-compose.yml`: the `postgres` service gets `restart: unless-stopped`, so a container created once comes
  back whenever Docker Desktop runs (Open О1).

### 7. Diagnostics UI («Настройки → Диагностика», block «Hub на этом компьютере»)
Quiet status (icon + coloured text, no fills): «Работает» / «Запускается…» / «Не запущен: <reason>» / «Работает, но
база недоступна: <reason>» / «Используется Hub по адресу <url>» (remote). Reasons (closed list, Russian texts in the UI
task): `no_provider_key` «введите ключ провайдера в «Подключениях»» (link), `no_database_url`, `no_hub_exe` «hub.exe нет
рядом с приложением: пересоберите бету», `docker_off` «включите Docker Desktop», `database_down` «Docker запущен, но база
Hub не отвечает: запустите контейнер postgres», `port_busy` «порт <n> занят другой программой», `exited` «Hub
завершился (код <n>), подробности в логе», `config_error` (exit 2). Actions: «Проверить снова», «Перезапустить Hub»
(only when ours runs), «Открыть папку с логами» (opens `<DataDir>/logs` through the existing explorer opener; fixed
path, never from the page). Every reason has a next step (no dead ends).

## Consequences
- The owner double-clicks «paratech (beta)»; with Docker Desktop on, the Hub is there. The cloud path and the student
  build are unchanged.
- Security review (`docs/process/security-review.md`) is required for the spawn/stop/key-issuance adapter and the
  build script: fixed path, no PATH lookup, no shell, secrets only in the child env, stop only our image, loopback-only
  listen, nm check of the student exe.
- New code in the desktop module only behind the tag at link time; CI gains `-tags beta` vet/test.
- No new dependency (`x/sys/windows` already used). PRD unchanged (beta tooling, PRD §12 keys still guard the Hub).
- Agent parity (ADR-0033): UI-only — a secret is typed; restart and diagnostics are beta tooling, not a student
  feature (row in `docs/agent-parity.md`).
- Tasks: TASK-385 build script and CI, TASK-386 Hub log file and compose restart, TASK-387 domain and use case,
  TASK-388 adapters, TASK-389 wiring and binding, TASK-390 restart and key issuance, TASK-391 UI.

## Open (вопросы владельцу)
- **О1.** Postgres в Docker: приложение само Docker и контейнеры не запускает. Предлагаю один раз добавить контейнеру
  `restart: unless-stopped` — тогда база поднимается сама, как только запущен Docker Desktop. Подходит, или приложению
  можно самому выполнить `docker compose up -d postgres`, когда Docker Desktop уже запущен?
- **О2.** Если приложение не смогло отделить Hub от себя (Windows не разрешила), Hub закроется вместе с приложением.
  Это допустимо (с записью в логе), или нужно что-то другое?
</content>
</invoke>
