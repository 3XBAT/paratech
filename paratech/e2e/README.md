# paratech/e2e

Test tool of ADR-0035: a throwaway real Hub in front of a scripted fake Anthropic Messages API (SCRIPTED) or the real
provider (LIVE). Talks to the Hub only through processes and HTTP; no Go code, nothing imported from `hub/` or `desktop/`.

## Run

```
npm ci                       # inside paratech/e2e (npm 10 ignores --prefix for a bare install)
npm --prefix <abs>/paratech/e2e test    # tsc + node --test: unit tests, and the real Hub test when Docker is up
```

The Hub test builds `paratech/dist/e2e/hub.exe` (`go -C paratech build ./hub/cmd/hub`, once per process), starts the
`postgres` service of `hub/docker-compose.yml` under its own compose project, creates schema `e2e_<run>`, issues a key
with `hub keys create -label e2e-<run>`, serves on a free `127.0.0.1` port and calls `GetRemaining` and one `Complete`
answered by the fake. It is skipped without Docker (and without `HUB_TEST_DATABASE_URL`) or with `PARATECH_E2E_UNIT_ONLY=1`.

```ts
const fake = await startFakeLLM();
fake.setScript([{ kind: "tools", match: "какие у меня записи", reply: [{ name: "list_recordings", input: {} }] }]);
const hub = await startHub({ mode: "scripted", fakeUrl: fake.url }); // { url, apiKey, schema, logs(), stop() }
// ... fake.requests(), fake.unmatched()
await hub.stop(); await fake.stop();
```

## Variables

| Variable | Use |
|---|---|
| `PARATECH_E2E_PG_PORT` | Postgres port of the E2E compose project; else `HUB_PG_PORT`; else 55432 |
| `HUB_TEST_DATABASE_URL` | use this database instead of compose (`sslmode=disable`) |
| `PARATECH_E2E_ANTHROPIC_KEY` | LIVE only: provider key (required) |
| `PARATECH_E2E_ANTHROPIC_BASE_URL`, `PARATECH_E2E_ANTHROPIC_AUTH` | LIVE only: gateway URL and `x-api-key`/`bearer` |
| `PARATECH_E2E_UNIT_ONLY=1` | skip the real Hub test |

`startHub` options: `mode`, `fakeUrl` (SCRIPTED), `quotaLimit` (default 400000 tokens), `logLevel`, `env`, `readyTimeoutMs`.
Environment problems throw typed `E2EEnvironmentError`s with a `kind` (`DockerNotRunning`, `PortBusy`, `ForbiddenPort`,
`MissingLiveKey`, `HubBuildFailed`, `HubNotReady`, ...) and a Russian message: a run reports them as «не проверено».

## Fake Messages API

`node:http` on `127.0.0.1`, unary only (the Hub runs with `HUB_PROVIDER_STREAM=false`). Rules
`{kind: "tools"|"plain", match, reply, text?, usage?}`: a request takes the first remaining rule whose `match` is a
substring of the last user message (text and `tool_result` text); the rule is consumed. No rule: HTTP 500 `api_error`,
listed as unmatched («unscripted LLM call»). Control: `POST /__e2e/script` `{"rules": [...]}` (strictly validated, 1 MiB
cap; replaces the script and clears the recordings), `GET /__e2e/requests`, `GET /__e2e/unmatched`. Recordings hold
request bodies only, never headers.

## Isolation

- Own compose project `paratech-e2e` (own container, network, volume): the owner's stack is never touched. A busy port
  is an error, never reused; only a running postgres of `paratech-e2e` itself is reused. `stop()` runs
  `docker compose -p paratech-e2e down -v` only if this process started the stack.
- The Hub never listens on 8080 or 58080, and only on `127.0.0.1`.
- The Hub process gets a minimal environment: `PATH`, `SystemRoot`, `windir`, `TEMP`, `TMP` (`TMPDIR`, `HOME`
  elsewhere) and its own `HUB_*` settings, so no `HUB_*`/`PARATECH_*` value of the owner leaks in. `go` and `docker`
  inherit the environment without `HUB_*` and `PARATECH_*` (they need the toolchain setup), so they never see the key.
- The live key goes only into the Hub's environment: never argv, files or messages. Captured Hub output, error
  messages, `JSON.stringify(hub)` and `inspect(hub)` are scrubbed of the live key, the run's API key, the seal keys and
  the database password; `hub.apiKey` is the only secret returned (not enumerable, masked in JSON).
- A leftover from a crashed run: `docker compose -p paratech-e2e -f paratech/hub/docker-compose.yml down -v`.
