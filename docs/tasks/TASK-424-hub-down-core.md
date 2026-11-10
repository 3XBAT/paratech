# TASK-424: «Сервер не отвечает» — classification, Connections status, «Проверить снова» for waiting jobs

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** the bugfixer branch `ralph/20261110-bug-ci-flakes` merged (it touches `adapters/hubclient` tests)
- **PRD sections / AC:** §7.2, §9.2, §12, AC-12; ADR-0037 (decisions 1–4), ADR-0036 (no overlap required)
- **Агент:** только UI: «Проверить снова» rechecks the queue, no student data changes (ADR-0033 §1)
- **Branch:** ralph/<date>-task-424

## Goal
A Hub URL that is set but where nothing answers is told apart from «no internet» in the log, the job rows and
«Подключения», and waiting jobs can be rechecked at once.

## Step 0 (check, record in progress with screenshots)
Run the app with «Адрес сервера» `http://127.0.0.1:<free port>` and a key, nothing listening: what the agent panel,
a recording row in WAITING_FOR_NETWORK, «Подключения» → Hub and `desktop.log` show. Same with a dial timeout
(a blackhole address such as `https://10.255.255.1`). This is the «before» of TASK-426.

## Allowed to change
- `paratech/desktop/internal/domain/shared/errors.go` (`ErrServiceDown`) + test
- `paratech/desktop/internal/adapters/hubclient/errors.go`, `refused_*.go`, `client.go` (only to pass the URL host to
  the classifier) + tests
- `paratech/desktop/internal/cases/connections/*` (`ErrHubDown`, `View.HubLocal` via `domain/localhub/values.ParseTarget`),
  the quota outcome in its domain values package (`QuotaHubDown`) + tests
- `paratech/desktop/internal/app/connections.go` (mapping) + test
- `paratech/desktop/internal/input/wails/connections.go` (status `hubDown`, `hubLocal`, refresh text) + test
- `paratech/desktop/internal/cases/jobrunner/*` (`RecheckNow`) + tests
- `paratech/desktop/internal/input/wails/activity.go` (`RecheckNetwork`) + test, `internal/app/*` (wiring only)
- `paratech/desktop/frontend/src/bindings/{connections,activity}.ts` (parse only) + tests

## Acceptance criteria
- [ ] hubclient: refused dial (any host) and any transport failure to a loopback host → reason `ErrHubDown`, matches
      `shared.ErrNetworkUnavailable`, `shared.ErrServiceUnavailable`, `shared.ErrServiceDown`; DNS failure, dial timeout,
      reset to a remote host → unchanged («no network»); not configured / key / quota precedence unchanged (table test
      with an `httptest` server closed, a closed loopback port, a fake dialer for timeouts).
- [ ] Jobs: a summary job against a stopped local Hub waits with cause `service_unavailable` (desktop/tests scenario);
      `jobrunner.RecheckNow` makes every WAITING_FOR_NETWORK job due at the next tick, uses no attempt, one recheck in
      flight; `ActivityService.RecheckNetwork` → `{ok}`.
- [ ] Connections: refresh against a down Hub → status `hubDown`, `hubLocal` true for `http://127.0.0.1:<port>`, the
      texts of ADR-0037 §4; «Нет связи», «Нет ключа», «Ключ не принят», quota unchanged.
- [ ] `desktop.log` line of such a call names `hub does not answer at its address`, without the key.
- [ ] `go test -race ./desktop/internal/adapters/hubclient/... ./desktop/internal/cases/connections/...
      ./desktop/internal/cases/jobrunner/... ./desktop/internal/input/wails/... ./desktop/internal/app/... ./desktop/tests/...`
      and the architecture test; `GOOS=windows go vet ./desktop/...`; `golangci-lint run ./desktop/...`;
      `npm --prefix paratech/desktop/frontend run test`.
