# TASK-386: Hub logs to a size-rotated file (HUB_LOG_FILE); compose Postgres restarts with Docker

- **Agent:** hub-dev (opus)
- **Label:** hub
- **Depends on:** none
- **PRD sections / AC:** ADR-0036 §6; ADR-0003 (linear main), ADR-0009 (desktop log file pattern)
- **Агент:** не касается (Hub)
- **Branch:** ralph/<date>-task-386

## Goal
A Hub started by the beta writes its log to `<DataDir>/logs/hub.log` with rotation; without the setting it logs to
stderr as today.

## Allowed to change
- `paratech/hub/config/config.go`, `config/*_test.go`
- `paratech/hub/internal/adapters/logfile/*` (new: size rotation, 5 MiB, `hub.1.log`, `hub.2.log`; std only)
- `paratech/hub/internal/app/*` (attach the sink), `paratech/hub/cmd/hub/main.go` (only if a linear step is needed)
- `paratech/hub/docker-compose.yml` (`restart: unless-stopped` on `postgres` only)
- `paratech/README.md` (Hub config table: one row), `paratech/hub/tests/*` (+ tests)

## Out of scope
- Desktop code.

## Acceptance criteria
- [ ] Config `HUB_LOG_FILE` / `-log-file`: absolute path or empty; a relative path is a config error; shown in
      `String`/`LogValue` (not a secret).
- [ ] When set, `serve` logs only to the file, attached right after the listener is bound (a second Hub on the same
      port fails `listen` before touching the file); before that, stderr. Key commands ignore it.
- [ ] Rotation: at 5 MiB the file becomes `hub.1.log`, `hub.1.log` → `hub.2.log`, the oldest is dropped; works while
      the Hub runs on Windows (rename of a closed file; test on Windows).
- [ ] No secret in the file: a test greps the file after a start with a known provider key, DB password and seal
      secret.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race ./hub/...` (DB tests with `HUB_TEST_DATABASE_URL`), arch test,
      golangci-lint v2.14.0.
</content>
</invoke>
