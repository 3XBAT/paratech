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
bash <ops>/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @settings   # one pack (here: «Настройки и подключения»)
npm --prefix <abs>/paratech/e2e run fetch-model                                   # ggml-base-q5_1.bin into paratech/dist/cache/e2e (a run does this by itself)
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
| `src/checks` | layout checker (`checkLayout`, `layoutSession`, page probe, pure `evaluateLayout`, console watch, agent-thread overlap, synthetic ICS) |
| `src/fixtures` | pure builders and readers of the packs: the synthetic week (`schedule.ts`), the model cache and the corrupted copy (`model.ts`), the pick-queue guard (`picks.ts`), text readers (`text.ts`, `files.ts`) |
| `scripts` | `fetch-model.ts` (compiled by tsc like the rest, `npm run fetch-model`) |
| `specs/<pack>` | scenarios; `specs/smoke` is the `@smoke` start check, `specs/layout` the `@layout` pack (two window sizes: `PARATECH_E2E_WINDOW=1024x640`, `docs/process/e2e.md`), `specs/settings` the `@settings` pack (below) |

Ctrl+C during the setup: Playwright still runs the global teardown, and the setup's handle is in a global slot from the
start, so the teardown stops what is already up (the app as soon as it is spawned, not only once it is proven isolated)
and the setup stops whatever comes up after that. On the global timeout the teardown starts on an already expired
deadline and Playwright does not wait for it, so there it is best effort and the exit guard is the backstop. The exit
guard runs at every process exit, also after a teardown (it is idempotent). If the process dies without a teardown (a
crash, a kill), the guard kills the app tree and the Hub, but not the compose project; check for leftovers:
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

| `PARATECH_E2E_UNIT_ONLY=1` | skip the real Hub test |
| `PARATECH_E2E_WINDOW=WxH` | `--e2e-window` of the app (default: the app's own size) |
| `PARATECH_E2E_MODE`, `PARATECH_E2E_NO_BUILD` | set by `cli.js` from `--live` / `--no-build`; do not set by hand |
| `PARATECH_E2E_RUN_ROOT`, `…_CTL_DIR`, `…_DATA_DIR`, `…_CDP_URL`, `…_FAKE_URL`, `…_HUB_URL`, `…_HUB_KEY`, `…_HUB_DATABASE_URL` | outputs of the global setup for the workers (`src/contract.ts`); the Hub key and the Hub database URL live in `process.env` only (never `state.json`) and are scrubbed from reports |

The three provider variables are removed from `process.env` as soon as the Hub has started (the workers inherit it), but
their values stay registered with the report scrubber of the main process, so they never reach a report or an error text.

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

## Settings pack (`specs/settings`, `@settings`, TASK-377)

Sets the sandbox up through the real UI and proves the Hub connection, the model import and the schedule import. Ten report
rows, in file order (one worker; `hub.spec.ts` < `model.spec.ts` < `schedule.spec.ts`; later rows say `requires`):

- **Hub** (AC-12, AC-25): address (the folded «Адрес сервера») and key typed into the UI, the quota line shows the E2E limit
  400 000; a **revoked key** shows the rejection. The key of the run must stay valid for the later packs, so the spec issues a
  SECOND key with `hub keys create` and revokes that one (`src/hub/keys.ts`: `keyTools().issueKey / revokeKey / listKeys`; it
  runs the `paratech/dist/e2e/hub.exe` of the run against `PARATECH_E2E_HUB_DATABASE_URL`); then the key of the run is typed
  back (the `finally` of that row is a first attempt) and a third row (`requires` the first) types the key of the run
  itself once more and confirms the quota line works again, so the later packs always get a valid key.
- **Model** (AC-07, AC-23): `ggml-base-q5_1.bin` is fetched once into `paratech/dist/cache/e2e` by the spec's `beforeAll`
  (`scripts/fetch-model.ts` runs the same code: size, SHA-256 and URL are read from `desktop/internal/app/catalog.json` at run
  time, `.part` file renamed only after the hash matched). `ModelFetchError.kind` decides the report: only `network` (no
  connection, HTTP error, the 9-minute download deadline) is «не проверено»; `mismatch` (size or SHA-256 differs from the
  catalog, or more bytes than the catalog size arrive: the download is cut off and the `.part` removed) and `catalog` (a
  broken `catalog.json`) are defects and fail the row. The FIRST run downloads the 57 MB model and that time counts against
  the pack budget (the spec's `beforeAll` allows 10 minutes); later runs reuse the verified cache. Redirects
  (HuggingFace -> CDN) are followed; integrity is enforced only by size + SHA-256 from the catalog. The file
  is placed under `<ctl>/fixtures/models/`. A copy with ONE byte changed (`<ctl>/fixtures/models-corrupt/`) is refused by
  «Импортировать скачанный файл…» of the catalog row with «Контрольная сумма SHA-256 не совпадает…»; «Импортировать файл
  модели…» shows the file's real SHA-256 (equal to the catalog's for the good file); then the good file is imported from the
  catalog row, made the default, and «Модуль расшифровки» is ready: the row asks the real `SpeechService.Status` through the
  Wails runtime of the page (`evalInPage`) until `state === "ready"`, then checks that the page shows no install notice
  (the notice renders nothing while its own status call is pending, so the DOM alone would prove nothing).
- **Schedule** (AC-16): `src/fixtures/schedule.ts` builds `<ctl>/fixtures/schedule/week.ics` at run time (Матанализ,
  Литература, История; one lesson at the first whole minute at least 2 min after the file is written; UTC `Z` times plus
  `X-WR-TIMEZONE:Europe/Moscow`). The preview shows subjects, kind, teacher and times in the zone of the file; «Применить»
  commits (the «Расписание» screen then shows the result line and the offer to create subjects; it keeps no lesson list);
  the lesson is on «Сегодня» (if the machine's local midnight falls before the lesson, that part is skipped and the row is
  «не проверено», after every other assertion of the row); the same file again changes nothing. Then the three subjects are created from the
  schedule and the synthetic call link `https://telemost.yandex.ru/j/00000000000001` is saved for «Матанализ» on «Сегодня».
  The ICS importer ignores `LOCATION` and the rest of `DESCRIPTION` (`icsschedule/doc.go`), so the link is added on «Сегодня»;
  it is never opened (only a later pack's click on «Подключиться» writes it to `opened.jsonl`).
- The pack leaves the app configured for the next packs: Hub address + key of the run, the model installed and default, the
  schedule and the three subjects with the link of «Матанализ».
- Every wait is on UI state with an explicit timeout; page code goes through `evalInPage`. The key is never put into a title,
  an annotation, a note or a log.

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
