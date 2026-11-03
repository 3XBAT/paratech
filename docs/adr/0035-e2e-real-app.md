# ADR-0035: End-to-end tests of the real desktop app

- **Status:** Proposed
- **Date:** 2026-11-04
- **Source:** owner 2026-11-03/04 (the supervisor tests the real app per feature and says «готово / сломано» before each
  beta; LIVE + SCRIPTED modes; Windows dev machine only; sandbox; synthetic fixtures; report with screenshots);
  TASK-025 (blocked: real whisper-cli run) moves here. ADR-0003, 0007, 0009, 0013, 0014, 0021, 0023, 0026.

## Context
All tests today stop below the UI: Go per layer, `desktop/tests` and `hub/tests` scenarios, frontend node tests (many
read source text by regex, no DOM). The owner found by hand: the window scrolled into an empty strip (CSS), the agent
sent the student to a screen without the links, invented a failure cause, could not see recording titles. Nothing
drives the real window. Facts checked for this ADR:
- Wails v3 beta.29 **clears** `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` in its loader `init` (`internal/webview2/
  webviewloader/env_create.go`), so the env var does not work; `application.Options.Windows.AdditionalBrowserArgs`
  (e.g. `--remote-debugging-port=N`) does (`webview_window_windows.go`). All windows share one WebView2 environment, so
  one CDP port exposes the main window and the popup (`index.html`, `popup.html`).
- Wails' single-instance mutex is `wails-app-<UniqueID>-sim`: an E2E launch with the app's id while the owner's app
  runs would exit and bring the owner's window forward. The WebView2 data dir defaults to the owner's
  `%APPDATA%\learning-assistant.exe`; the UI instance rewrites `HKCU\Software\Classes\paratech` at start (ADR-0013).
- A non-default `--data-dir` already gets an isolated Credential Manager profile, fail-closed (ADR-0014); the log says
  `secrets_profile=isolated`. So no Hub key, mail password or Google client of the owner is reachable.
- File choice uses native dialogs (`*picker_windows.go`), links and files open through `explorer.exe`, «Скопировать»
  writes the system clipboard: Playwright cannot drive the first and must not trigger the others for real.
- The Hub accepts `HUB_ANTHROPIC_BASE_URL=http://<loopback>` (a local fake), creates keys with `hub keys create`, and
  `hub/testenv.PostgresURL` already isolates tests by a schema in `search_path` on the Postgres of
  `hub/docker-compose.yml`. Process loopback is not available on the owner's 19045 (ADR-0002 audio capture): capture
  falls back to the default render device, which is audible.

## Decision

### 1. Two modes, one set of scenarios
- **SCRIPTED** (every task, free, deterministic): real `paratech.exe` + real local Hub + real Postgres + a **fake
  Anthropic Messages API** (Node, loopback) with scripted answers. It checks the app's side: tools run, cards render,
  navigation targets exist, and what the model *was given* (the fake records every Hub→provider request, so «the
  agent sees recording titles» and «the tool error reached the model with its real reason» are exact assertions).
- **LIVE** (before each beta, costs tokens): the same Hub with the real provider key; a tagged subset (`@live`) with
  loose assertions and rubrics (§6). It checks the model and the prompts. The E2E Hub key gets
  `HUB_QUOTA_LIMIT=400000` tokens, so one run can never spend more (owner question О4).

### 2. Driving the UI
- **Playwright** (`@playwright/test`, new devDependency of `paratech/e2e/` only; exact version pinned after
  `npm view`, to verify) with `chromium.connectOverCDP("http://127.0.0.1:<port>")`; no Playwright browser download.
  Pages are found by URL (`index.html` main, `popup.html` popup). Whether a hidden or occluded WebView2 window still
  renders and screenshots is **to verify** in TASK-375 and written to `docs/refs/playwright-webview2.md`.
- The popup is reached the real way: a synthetic schedule with a lesson starting in about a minute (AC-01 path).
- Native dialogs, opener and clipboard are replaced in E2E mode only (§3); nothing else is mocked inside the app.

### 3. E2E mode of the desktop app
Flags exist only in a build with tag **`e2e`** (`config/e2e_on.go`; `e2e_off.go` registers none), so the shipped
`-tags production` exe cannot open a debug port or skip a dialog (owner question О1). `build-alpha.ps1 -E2E` builds
`-tags production,e2e` into `paratech/dist/e2e/` with the speech component, as for the beta.

| Flag | Effect |
|---|---|
| `--e2e-dir <abs>` | turns E2E mode on; control dir of the run (fixtures, pick queue, records) |
| `--e2e-cdp-port <n>` | `AdditionalBrowserArgs: --remote-debugging-port=n` (Chromium binds loopback) |
| `--e2e-window <WxH>` | main window size, for the layout checks at more than one size |

In E2E mode, always: Wails instance id `…desktop.e2e`; WebView2 data dir `<e2e-dir>/webview`; no URL scheme
registration; window title and tray tooltip end with « · E2E»; the Hub URL must be a loopback address (else the Hub
client reports «not configured»); start is refused when the data dir is the default one or the profile is not
isolated; log line `e2e mode on`. Adapter `internal/adapters/e2ehooks` (wired by `app` only in E2E mode):
- **pickers**: open dialogs take the next path from `<e2e-dir>/picks.jsonl` (only files under `<e2e-dir>/fixtures`,
  else «cancelled»); save dialogs return `<e2e-dir>/saved/<proposed name>`;
- **opener and clipboard**: `weblink`, the file opener and the clipboard append `{kind, value}` to
  `<e2e-dir>/opened.jsonl` instead of acting. `input/wails.Options` gets these as optional overrides; ARCH-40/50 hold.
Data dir `<e2e-dir>/data`, `--log-level debug` (the runner reads `secrets_profile_id` to delete the profile's
Credential Manager entries at teardown, ADR-0014 consequences).

### 4. Hub and data
- Postgres = the `postgres` service of `hub/docker-compose.yml` (Docker on the dev machine, owner question О5), a fresh
  schema per run via `search_path`, dropped at teardown. The runner builds `hub.exe`, runs `hub keys create -label
  e2e-<run>` (key kept in memory, masked in reports), then `hub serve` on a free loopback port (never 8080 or 58080),
  random `HUB_AGENT_SEAL_KEYS`, `HUB_PROVIDER_STREAM=false` in SCRIPTED (the fake answers unary; streaming is covered by
  LIVE), dummy `HUB_ANTHROPIC_API_KEY` + fake URL in SCRIPTED, real key and URL/auth from the owner's environment in
  LIVE (`PARATECH_E2E_ANTHROPIC_KEY`, `…_BASE_URL`, `…_AUTH`; owner question О2). The provider key reaches only the
  Hub process; never the desktop, a log or a report.
- **Fake Messages API**: rules `{kind: tools|plain, match: substring of the last user content, reply: tool_use calls |
  text, usage}`; each rule is consumed in order; the active script is set per scenario through `POST /__e2e/script`;
  a request that matches no rule gets HTTP 500 and fails the scenario as «unscripted LLM call». Its wire format is
  checked against `hub/internal/adapters/anthropic` and `docs/refs/anthropic.md`.
- Desktop setup goes through the UI (it is a scenario too): «Адрес сервера», the key, model import, schedule import.
- **Fixtures** (`paratech/e2e/fixtures/`, no owner data): one public-domain Russian speech clip ≤ 60 s (LibriVox,
  source and licence in its README; owner question О3), synthetic ICS schedule, subjects, notes, assignments. The ggml
  model `base-q5_1` (59.7 MB, SHA-256 of `catalog.json`) is downloaded once into `paratech/dist/cache/e2e/` and checked.
- **Audio**: the main chain imports the clip through «Импортировать запись» (deterministic). A capture scenario
  (popup → «Начать запись» → the runner plays the clip on the default device → stop) is opt-in `--with-sound`: it is
  audible and depends on the device. Real whisper-cli runs in both; TASK-025 is closed by the recording pack (parse,
  cancel, speed in the report).

### 5. Layout invariants (checked on every screen visit, both modes)
`document.scrollingElement.scrollHeight <= innerHeight` (the window never scrolls, inner panes do); no element wider
than the viewport and `body.scrollWidth <= innerWidth`; every button or link without visible text has `aria-label`
and `title`; status elements have no coloured background or border (TASK-367); no raw `undefined`/`NaN`/`[object`/
English error text; no console error or unhandled rejection. Screens: every rail section, every settings item, the
subject page tabs, the lesson tabs, the assignment page, the agent panel open, the popup.

### 6. Scenario packs, report, budget, flakes
- Packs (`paratech/e2e/specs/<pack>/`): settings and connections, recording (import → STT → summary → assignment),
  popup and capture, assignments, agent (link question, «что было на паре», «добавь предмет по последней записи» + undo,
  tool failure honesty, «какие у меня записи» sees titles), layout. LIVE rubrics: the answer contains the synthetic
  link or opens a screen that shows «Подключиться»; after a scripted tool failure the reply names no cause from a
  deny-list absent from the tool result; titles of the synthetic recordings appear.
- One sandbox per run, packs in a fixed order, Playwright `workers: 1`; a pack whose prerequisite failed is «не
  проверено (нужно: …)», not «сломано».
- **One command**: `bash <ops>/heavy.sh npm --prefix <abs>/paratech/e2e run e2e` (SCRIPTED, builds unless
  `--no-build`), `run e2e:live`, filter `-- --grep @agent`. Budget: SCRIPTED ≤ 15 min, a pack ≤ 5 min; LIVE ≤ 30 min.
- **Report** `paratech/dist/e2e-run/report/report.md` (+ screenshots): table «Функция | Режим | Итог (готово / сломано
  / не проверено) | Шаг, где сломалось | Снимок», the STT speed, LIVE token use; readable by the supervisor and owner.
- **Flakes**: SCRIPTED has no retries (a flake is a bug: task, fixed or quarantined with `test.fixme` and a reason);
  LIVE retries a scenario once and reports «готово после повтора». Waits only on UI state, never fixed sleeps.
- **Adding a scenario**: a UI task that changes a feature adds or updates its scenario in its pack (`docs/process/e2e.md`).

### 7. Where the code lives
`paratech/e2e/` (TypeScript, own `package.json`, no Go code, imports nothing from `desktop/` or `hub/`; talks to them
only through processes, CDP, files of `<e2e-dir>` and HTTP). It is a test tool, not a service, so ARCH-01 does not
apply and the arch tests are unchanged. Outputs under `paratech/dist/` (ignored). Linux CI only type-checks and unit
tests `paratech/e2e` (matcher, report); it cannot run WebView2.

## Consequences
- The supervisor runs SCRIPTED per merged batch and LIVE before each beta; `docs/process/supervision.md` gets the step
  (TASK-382). `coordinator-round.md` (paratech-ops) should require the touched pack: follow-up for the supervisor.
- The E2E exe differs from the beta exe only by the `e2e` tag files; CI vets and tests `-tags e2e` too.
- A local CDP port during a run lets any local process drive the E2E window; it holds only synthetic data and a
  throwaway Hub key.
- Not covered in Sprint 1: mail sending (no SMTP fake), Google Calendar, OS drag-and-drop, several monitors/DPI.

## Open (вопросы владельцу)
- **О1.** Переключатели E2E — только в отдельной сборке с тегом `e2e` (рекомендую) или прямо в выпускаемом exe?
- **О2.** Ключ LLM для LIVE: переменные окружения пользователя `PARATECH_E2E_ANTHROPIC_KEY` (+ `_BASE_URL`, `_AUTH`
  для шлюза) (рекомендую) — или другой источник?
- **О3.** Звук: закоммитить отрывок ≤ 60 с из LibriVox (общественное достояние) (рекомендую) или генерировать речь
  голосом Windows; сценарий записи со звуком из колонок — только по флагу `--with-sound`?
- **О4.** Предел LIVE-прогона 400 тыс. токенов (≈ 1,6 % из 25 млн) — подходит?
- **О5.** Postgres для E2E — Docker (`hub/docker-compose.yml`) на рабочей машине — подходит?
- **О6.** Окно E2E видно на экране во время прогона (скрытое окно может не рисоваться, проверим) — допустимо?
