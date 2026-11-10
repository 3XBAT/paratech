# ADR-0037: «Сервер не отвечает» is told apart from «нет связи»

- **Status:** Proposed (architect 2026-11-10, «agent quality» sprint; texts Open, see below)
- **Date:** 2026-11-10
- **Source:** owner 2026-11-10: the Hub at `http://127.0.0.1:8080` was down after a reboot (no `hub.exe`, Postgres
  container exited); `desktop.log` said only `hubclient: … network unavailable`, summaries and the agent waited
  silently. Related: TASK-083 (job cause), ADR-0026 addendum 2026-11-08 (offline vs failed), ADR-0036 (beta local Hub).

## Context
Facts in the code (10.11):
- `adapters/hubclient/errors.go` `mapCode`: a `net.Error` under `CodeUnavailable` is `shared.ErrNetworkUnavailable`;
  only a **refused dial** also gets `shared.ErrServiceUnavailable`. A dial timeout or a reset before any answer to a
  Hub on this computer reads as «no network». The error text of both is `<op>: network unavailable`, so the log
  cannot tell them apart.
- Jobs: `backoff` maps `ErrServiceUnavailable` to job cause `service_unavailable` (migration 0010, CHECK on the
  column), the recording row shows «Сервис конспектов недоступен» / «Повторится автоматически. Запись сохранена.»;
  no way to ask for a check now (jobs wait for `Rechecks`).
- Agent: every transport failure stops the request with `values.StopOffline` (persisted); the card says «Не завершено:
  нет связи.» and the field shows «Нет связи с paratech». Nothing says the server itself is down.
- «Подключения», card Hub: quota status `offline` → «Нет связи»; the refresh text says «проверьте подключение к
  интернету и адрес Hub» also when the Hub process simply is not running.

## Decision
1. **Class «hub_down»** (rule in `adapters/hubclient`, ARCH-31/32): a call that got no answer at all is «hub_down» when
   the dial was refused, or when the host of the Hub URL is loopback (`127.0.0.0/8`, `::1`, `localhost`) whatever the
   transport failure (refused, dial timeout, reset before the first byte). The error matches, as today,
   `shared.ErrNetworkUnavailable` (the job parks without using attempts) and `shared.ErrServiceUnavailable`, and a new
   domain sentinel **`shared.ErrServiceDown`** («the service does not answer at its address»). Its reason is a new
   `hubclient.ErrHubDown` (text `hub does not answer at its address`), so `desktop.log` names it. Other transport
   failures to a remote host stay «no network». Not configured, rejected key and quota keep their precedence.
2. **No new persisted state.** Job cause stays `service_unavailable` (no migration); agent stop reason stays
   `offline`. The agent use case adds a transient `OfflineCause` (`network` | `hub_down`) to the step it returns, from
   `errors.Is(err, shared.ErrServiceDown)`; the Driver's update and `agent:update` carry it. A chat reopened after a
   restart shows the generic offline text (accepted).
3. **Local or remote** is read from the saved Hub URL with `domain/localhub/values.ParseTarget` (the same notion of a
   local Hub as ADR-0036) in `cases/connections` (`View.HubLocal`); the bindings pass it to the page. No rule in React.
4. **Surfaces** (texts in the UI task; defaults below, Open О1):
   - Agent card (stop `offline`, cause `hub_down`): «Не завершено: сервер paratech не отвечает.» + hint local «Сервер
     на этом компьютере не запущен. Запустите его и нажмите «Повторить».», remote «Интернет есть, сервер не отвечает.
     Можно повторить позже.»; buttons as today (`retry`, `cancel`). Cause `network`: unchanged.
   - Recording and job rows (cause `service_unavailable`): label «Сервер paratech не отвечает»; hint local «Сервер на
     этом компьютере не запущен. Запись сохранена, обработка продолжится, когда он ответит.», remote «Запись сохранена,
     обработка продолжится сама, когда сервер ответит.»; way out: icon action «Проверить снова» (tooltip) =
     `ActivityService.RecheckNetwork` → `jobrunner.Service.RecheckNow`: every `WAITING_FOR_NETWORK` job is due at the
     next tick (no attempt used, one recheck in flight).
   - «Подключения», card Hub: quota status `hubDown` → «Сервер не отвечает»; refresh text «Hub по адресу <адрес> не
     отвечает.» + local «Если он запускается на этом компьютере, запустите его и нажмите «Обновить».», remote «Интернет
     есть: сервер выключен или адрес неверный. Можно обновить позже.» Distinct from «Нет связи», «Нет ключа», «Ключ не
     принят», quota.
5. **Beta (ADR-0036).** The local Hub block keeps its own diagnosis and «Проверить снова»; when
   `bindings/localhub.ts` says available (TASK-389/391), the local hints above add the link «Диагностика». Until then
   nothing of ADR-0036 is required.

## Consequences
- Tasks: TASK-424 (classification, connections, recheck), TASK-425 (agent cause), TASK-426 (UI texts and the action).
- `domain/shared` gains one sentinel; `connections` gains an outcome; `jobrunner` a method; no migration, no Hub change,
  no dependency.
- Agent parity (ADR-0033): «Проверить снова» is UI-only (a recheck of the queue, not a student action on data).

## Open (вопросы владельцу)
- **О1.** Naming: the panel and rows say «сервер paratech», «Подключения» keep «Hub» (as the page does today). Default:
  as written.
- **О2.** «Проверить снова» on a row: icon + tooltip (default, owner rule on small actions) or a word button.
