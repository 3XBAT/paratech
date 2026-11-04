# paratech/e2e

Test tool of ADR-0035: a throwaway real Hub in front of a scripted fake Anthropic Messages API (SCRIPTED) or the real
provider (LIVE). Talks to the Hub only through processes and HTTP; no Go code, nothing imported from `hub/` or `desktop/`.

## Run

```
npm --prefix <abs>/paratech/e2e ci      # first time (npm 10.9 honours --prefix for ci: checked with --dry-run)
npm --prefix <abs>/paratech/e2e test    # tsc + node --test: unit tests, and the real Hub test when Docker is up

# the whole run holds the machine lock, so always through heavy.sh (never nested inside the runner):
bash <ops>/heavy.sh npm --prefix <abs>/paratech/e2e run e2e                       # SCRIPTED: builds the E2E exe, runs every spec
bash <ops>/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @smoke      # a tag or title filter (any `playwright test` argument)
bash <ops>/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --no-build         # reuse paratech/dist/e2e/paratech.exe
bash <ops>/heavy.sh npm --prefix <abs>/paratech/e2e run e2e:live                  # mode LIVE (= e2e -- --live; the real provider key is needed)
```

`npm run e2e` compiles with `tsc` and starts `build/src/cli.js`, which takes `--no-build` and `--live` and hands the
rest to `playwright test` (it runs the TypeScript sources itself; the `.js`-suffixed imports resolve for tsc and for
Playwright alike). No browser is ever downloaded or launched: specs attach to the app over CDP.

One run, in order (`src/sandbox/run.ts`): wipe the run root `paratech/dist/e2e-run/` (only that exact folder; a locked
file is an environment error), create `ctl/` (= `--e2e-dir`), `ctl/data` (= `--data-dir`), copy `fixtures/` into
`ctl/fixtures` → fake LLM → Hub → build (unless `--no-build`) → start the app with a minimal environment and wait for the
four log lines → check the debug port is loopback-only → specs → teardown: hard stop of the app tree
(`taskkill /T /F`), Hub, fake, and the Credential Manager entries `paratech-<secrets_profile_id>/…` of this profile only.
The sandbox files stay after the run (the next run wipes them); the report is `paratech/dist/e2e-run/report/report.md`.

| Folder | Content |
|---|---|
| `src/sandbox` | run root, wipe guard, build, Credential Manager cleanup, global setup and teardown, `state.json` |
| `src/app` | launching the exe (`launch.ts`), `netstat` loopback check, `connectOverCDP` (`cdp.ts`) |
| `src/steps`, `src/report` | spec helpers (`pick`, `opened`, `llm`, `step`, page stubs) and the reporter (`docs/process/e2e.md`) |
| `src/hub`, `src/fakellm` | the Hub harness and the fake Messages API (below) |
| `specs/<pack>` | scenarios; `specs/smoke` is the `@smoke` start check |

Ctrl+C or the global timeout during the setup: Playwright still runs the global teardown, and the setup's handle is in a
global slot from the start, so the teardown stops what is already up (the app as soon as it is spawned, not only once it is
proven isolated) and the setup stops whatever comes up after that. If the process dies without a teardown (a crash, a kill),
an exit guard kills the app tree and the Hub, but not the compose project; check for leftovers:
`paratech.exe` / `hub.exe` of the run (`tasklist`), and `docker compose -p paratech-e2e -f paratech/hub/docker-compose.yml down -v`.
`taskkill /T /F` runs only against an app that is still alive (the pid of an exited one may belong to another process,
for instance your own paratech.exe).
The next run also deletes the Credential Manager entries of the previous run's profile before it wipes the folder.
A failure before the first state (a malformed option, a locked file in `paratech/dist/e2e-run`) is an environment
problem too: the previous run's `state.json` is deleted first, and the reporter ignores a `state.json` older than itself.

Reports, `trace.zip` and screenshots under `paratech/dist/e2e-run/report` may contain text typed into the UI, among it
the throwaway Hub key of the run. Never share or attach them anywhere; they stay under `paratech/dist` (gitignored).

The Hub test builds `paratech/dist/e2e/hub.exe` (`go -C paratech build ./hub/cmd/hub`, once per process), starts the
`postgres` service of `hub/docker-compose.yml` under its own compose project, creates schema `e2e_<run>`, issues a key
with `hub keys create -label e2e-<run>`, serves on a free `127.0.0.1` port and calls `GetRemaining` and one `Complete`
answered by the fake. It is skipped without Docker or with `PARATECH_E2E_UNIT_ONLY=1`.

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
| `PARATECH_E2E_PG_PORT` | Postgres port of the E2E compose project; unset: a free loopback port is picked for each run (no fixed default: Windows reserves ranges, `netsh int ipv4 show excludedportrange protocol=tcp`). A running postgres of the project is reused whatever its port |
| `PARATECH_E2E_ANTHROPIC_KEY` | LIVE only: provider key (required); only the Hub process gets it |
| `PARATECH_E2E_ANTHROPIC_BASE_URL`, `PARATECH_E2E_ANTHROPIC_AUTH` | LIVE only: gateway URL and `x-api-key`/`bearer` |

The three provider variables are removed from `process.env` as soon as the Hub has started (the workers inherit it), but
their values stay registered with the report scrubber of the main process, so they never reach a report or an error text.
| `PARATECH_E2E_UNIT_ONLY=1` | skip the real Hub test |
| `PARATECH_E2E_WINDOW=WxH` | `--e2e-window` of the app (default: the app's own size) |
| `PARATECH_E2E_MODE`, `PARATECH_E2E_NO_BUILD` | set by `cli.js` from `--live` / `--no-build`; do not set by hand |
| `PARATECH_E2E_RUN_ROOT`, `…_CTL_DIR`, `…_DATA_DIR`, `…_CDP_URL`, `…_FAKE_URL`, `…_HUB_URL`, `…_HUB_KEY` | outputs of the global setup for the workers (`src/contract.ts`); the Hub key lives in `process.env` only |

The harness reads only `PARATECH_E2E_*` variables as inputs. `HUB_PG_PORT` is an output: `hub/docker-compose.yml`
interpolates it, so the harness sets it in the environment of the `docker compose` child. The app is started with
`PATH`, `SystemRoot`, `windir`, `TEMP`, `TMP` and `LOCALAPPDATA` (the app refuses an E2E start without it) and nothing else.

`startHub` options: `mode`, `fakeUrl` (SCRIPTED), `quotaLimit` (default 400000 tokens), `logLevel`, `env`, `readyTimeoutMs`.
Errors are typed, with a `kind` and a Russian message (scrubbed of every secret of the run), in two hierarchies:

- `E2EEnvironmentError` — the machine is not ready: `DockerMissing`, `DockerNotRunning`, `PortBusy`, `ForbiddenPort`,
  `MissingLiveKey`, `BadConfig`, `DatabaseUnavailable`, `GoToolchainMissing` (go cannot be started). A run reports
  them as «не проверено».
- `HubFailedError` — the Hub itself failed: `HubBuildFailed` (`go build` exited nonzero or timed out), `HubKeyFailed`
  (`keys create` gave no key), `HubNotReady` (no `/readyz` in time, exited at startup). These can be real Hub
  regressions: a run reports them as «сломано», never «не проверено».

`isEnvironmentError(e)` / `isHubFailedError(e)` tell them apart.

## Fake Messages API

`node:http` on `127.0.0.1`, unary only (the Hub runs with `HUB_PROVIDER_STREAM=false`). Rules
`{kind: "tools"|"plain", match, reply, text?, usage?}`: a request takes the first remaining rule whose `match` is a
substring of the last user message (text and `tool_result` text); the rule is consumed. No rule: HTTP 500 `api_error`,
listed as unmatched («unscripted LLM call»). Control: `POST /__e2e/script` `{"rules": [...]}` (strictly validated, 1 MiB
cap; replaces the script and clears the recordings), `GET /__e2e/requests`, `GET /__e2e/unmatched`. Any other path
is HTTP 404 and is recorded as unmatched too («unknown endpoint METHOD path»), so a new provider endpoint the Hub
starts calling is not missed. Recordings hold request bodies only, never headers. A request whose `Host` is not
`127.0.0.1:<port>` or `localhost:<port>` gets HTTP 403 (DNS-rebinding guard); the Hub and the specs use the literal URL.

## Решения / ограничения

- The fake returns no thinking blocks: SCRIPTED checks the app side (tools, quota, flows), not the model's reasoning.
- Rules match as «first remaining match»: the first not yet consumed rule whose `match` is a substring wins; order
  the rules from specific to general.
- An unmatched call is HTTP 500 `api_error`, which is transient for the Hub and the desktop: they may retry it, so
  one unscripted call can show up several times in `/__e2e/unmatched`.
- Linux CI must set `PARATECH_E2E_UNIT_ONLY=1`: there it only type-checks and unit tests this package (ADR-0035 section 7).
- The database is always the postgres service of the compose project `paratech-e2e`; no variable can point the harness at
  another database. `PARATECH_E2E_PG_PORT`, if set, must be a port of its own (never the owner's 15432).

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
