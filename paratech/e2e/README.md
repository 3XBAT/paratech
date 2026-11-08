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
| `src/app` | launching the exe (`launch.ts`), `netstat` loopback check, `connectOverCDP` with `reconnect()` (`cdp.ts`), the processes of the sandbox by image name and exe path (`procs.ts`), a hard stop and a new start of the app on the same sandbox (`restart.ts`) |
| `src/steps`, `src/report` | spec helpers (`pick`, `opened`, `llm`, `step`, page stubs) and the reporter (`docs/process/e2e.md`) |
| `src/hub`, `src/fakellm` | the Hub harness and the fake Messages API (below) |
| `src/checks` | layout checker (`checkLayout`, `layoutSession`, page probe, pure `evaluateLayout`, console watch, agent-thread overlap, synthetic ICS) |
| `src/fixtures` | pure builders and readers of the packs: the synthetic week (`schedule.ts`), the model cache and the corrupted copy (`model.ts`), the pick-queue guard (`picks.ts`), text readers (`text.ts`, `files.ts`), the audio variants, the speed line and the scan for audio in requests (`audio.ts`), the lecture of an import (`lecture.ts`) |
| `scripts` | `fetch-model.ts` (compiled by tsc like the rest, `npm run fetch-model`); `llm/<pack>/` the scripted provider answers of a pack |
| `specs/<pack>` | scenarios; `specs/smoke` is the `@smoke` start check, `specs/layout` the `@layout` pack (two window sizes: `PARATECH_E2E_WINDOW=1024x640`, `docs/process/e2e.md`), `specs/settings` the `@settings` pack, `specs/recording` the `@recording` pack (below) |

**Order of the packs.** `playwright.config.ts` has one project per pack, in the order layout, settings, recording, smoke (one
worker runs the groups of the projects in that order; without projects Playwright runs files alphabetically, which would put
`recording` before `settings`, whose Hub key, speech model and schedule it needs). A new pack is one `pack("<name>")` line in the
list (a unit test fails for a folder of `specs/` that is not in it). There is no `dependencies`: a red pack does not switch the
next ones off; a row that needs another says `requires`. `requires` may name several rows joined by « + »; all must be «готово».

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

## Recording pack (`specs/recording`, `@recording`, TASK-378)

A synthetic lecture goes the whole real way: «Записи» → «Импортировать запись» + a picked MP3 → the real `whisper-cli.exe` of the
exe folder with the default model → the transcript → three scripted Hub calls → the lesson screen and the assignment. It needs the
state of the settings pack (Hub key, speech model by default, the schedule and its subjects): every row `requires` them (rows of
the settings pack, joined by « + »), so `--grep "@settings|@recording"` runs both and a lone `--grep @recording` on a fresh
sandbox reports «не проверено (нужно: …)», not «сломано». Files run by name: `01-import`, `02-cancel`, `03-restart`, `04-privacy`.

| Row | What it checks |
|---|---|
| Запись: импорт и локальная расшифровка | the lecture (Литература, лекция, yesterday 10:00) is typed into «Дата и время» and the clip is picked (`fixtures/audio/…mp3`); the row goes through its statuses to «Конспект готов» without a restart; a `whisper-cli.exe` of the exe folder ran (found by image name AND exe path, with the model file in its command line) and `desktop.log` names the model of `transcription job enqueued` (AC-08); the speed line (below) |
| Запись: расшифровка содержит слова из речи | the lesson screen, «Расшифровка» holds at least 2 of the words of `fixtures/audio/README.md` («Expected words:», case-insensitive) |
| Запись: конспект из ответа Hub | «Конспект» shows the scripted summary and its key point |
| Запись: задание со сроком в «Задания» и «Сегодня» | the scripted extraction made an assignment with a deadline: on the lesson's «Задания», in «Задания», in «Сегодня» → «Сроки на неделе» |
| Запись: отмена расшифровки | a second import (other bytes, ~150 s of audio); while `whisper-cli.exe` runs the recording is deleted from its «⋯» menu; the recognition stops, no `whisper-cli.exe` of the sandbox is left, the row is gone, `desktop.log` says `outcome=cancelled`, nothing went to the Hub |
| Запись: перезапуск приложения во время расшифровки | a third import; during the recognition the app tree is killed (`taskkill /T /F`), started again on the same sandbox, data dir and debug port (`src/app/restart.ts`), the page reconnected (`app.reconnect()`), and the job finishes (AC-09): «Конспект готов» and the words are in «Расшифровка» |
| Запись: звук не уходит в Hub | AC-06 over ALL requests of the pack (the fake forgets them at each `script()`, so every test saves them to `<ctl>/recording-llm-requests.jsonl`): no `RIFF`, `OggS`, `ID3`, `fLaC`, no MPEG sync, no base64 run over 4 KiB or starting like a container; the scan is shown to find the real clip in base64 |

- **Hub calls.** One imported lecture makes three: summary, homework and lecture text (`homeworkflow/postprocess.go`). The fake
  picks a rule by a substring of the last user message, and the rules (`scripts/llm/recording/rules.ts`) tell the three requests
  apart by what only that request holds: `"segments":[` (lecture text), `"transcript_minutes"` (summary), `"transcript":"`
  (extraction, after the summary's rule). The answers are the strict JSON of `prompts.ParseSummary/ParseExtraction/ParseLectureText`;
  the lecture text is long enough not to be refused as «condensed» (at least 45 % of the transcript). The deadline of the
  assignment is in two days at 20:00 with the UTC offset of the lecture's start, as the app demands.
- **Why «Дата и время».** «Пара из расписания» offers only today's lessons that have begun; the settings pack's «Литература» is
  tomorrow. The lecture is given by date, time, subject «Литература» and kind.
- **Other bytes for every import.** The same SHA-256 returns the existing recording, so the clip is written again behind an
  ID3v2 tag with another title (`audioVariant`), three times in a row for the long ones. Under `<ctl>/fixtures/audio/variants/`
  (picks are taken only from `<ctl>/fixtures`); nothing is committed.
- **Cancel is the deletion.** The app has no «Отменить» for a running STT job: «Остановить» is for a recording that is being made,
  and «Фоновая обработка» lists jobs without a recording. A job is cancelled only by `recordingdelete` (the recording's «⋯» →
  «Удалить»), so the row has no «cancelled» state, it disappears. The row checks what that cancel promises (process, row, log,
  Hub) and the missing button is a finding for the owner.
- **Restart.** The setup records the pid of the app in `state.json` (`appPid`); `restartApp` kills that tree only while the pid
  still runs `paratech/dist/e2e/paratech.exe`, starts the same exe (log lines are counted from the end of the old log), writes
  the new pid (`appRestarted`), and the global teardown and the exit guard stop that process too (`killRestartedApp`). The
  app is left running for the next pack. It is spawned `detached` (and unref'd): libuv on Windows puts a non-detached child into a
  kill-on-close job object, so an app started by a worker would die when that worker exits (every project gets a new worker)
  and the next pack would get `connectOverCDP ECONNREFUSED`; the app is still stopped by pid with `taskkill /T /F`.
- **Speed line.** `AnnotationSttSpeed` (`stt-speed`) on the first row: «50 с звука расшифровано за N с (×K от реального времени);
  модель base-q5_1; процессор …»; the reporter prints it in «Скорость расшифровки» of `report.md`, for information.
- Processes are looked up with PowerShell `Get-CimInstance Win32_Process` (`src/app/procs.ts`), never by image name alone.

## Assignments pack (`specs/assignments`, `@assignments`, TASK-381)

The student's own assignment flows in the real window. Every row `requires` the settings rows it needs (the subject «История»
from «Расписание: предметы из расписания»; the draft and send rows also the Hub key), so both packs run with
`-- --project settings --project assignments` (not `--grep "@settings|@assignments"`: `npm run` hands the line to `cmd.exe`,
which reads the `|` as a pipe and only `@settings` runs) and a lone `--grep @assignments` on a fresh sandbox says
«не проверено (нужно: …)». Each row makes its own assignments
through the UI (unique titles, dates relative to the real clock) and deletes them in a `finally`, so a red row does not switch
the next one off. Files run by name: `01-add`, `02-change`, `03-done-remove`, `04-page`, `05-draft`, `06-send`, `07-journal`.

| Row | What it checks |
|---|---|
| Задание: добавление со сроком (с временем и без) | the form starts with time `20:00`; a date alone is stored as `20:00` (TASK-304), a typed `09:30` is kept; both stand in «На этой неделе» and in «Сегодня» → «Сроки на неделе», with «до <день>, ЧЧ:ММ» and the subject |
| Задание: просроченный срок не читается как «сегодня» | a deadline of yesterday and of today 00:00 read «просрочено, …», stand in «Просрочено», never match «сегодня/завтра до ЧЧ:ММ» and are not in «Сроки на неделе» (a control assignment proves the read is fresh) |
| Задание: смена срока переносит в другую группу | «⋯» → «Изменить» shows the stored date and time; +10 days moves it from «На этой неделе» to «Позже» and out of «Сроки на неделе» |
| Задание: «Выполнено» и возврат в работу | the check leaves «Открытые», the toast «Отменить» puts it back, «Выполненные» lists it with «Выполнено <дата>», it does not burn on «Сегодня», «Вернуть в работу» brings it back; the same with the buttons of the page |
| Задание: удаление через «⋯» и «Вернуть» | «⋯» → «Удалить» (no question: it appears only for a running draft) → «Задание удалено» + «Вернуть» → the row is back; a final delete leaves no row in any list and none in «Сегодня» |
| Задание: страница открывается без ошибок консоли и сети | console watch of `src/checks` (console errors, page errors, failed or 4xx/5xx requests; the 422 of `TurnkeyService.WorkOfAssignment`) while the page lives through one more poll of the core |
| Задание: быстрый черновик, правка, новая версия, файлы | AC-13: scripted draft in the fields, typing + «Сохранить правку» = version 2, «Дополнительные указания» + «Написать новую версию» = version 3, the Hub request carries the instruction and the edit; «Файл .txt» writes `homework.md` (content = draft), «Word (.docx)» writes `<ctl>/saved/<name>.docx` through the save hook (text = draft); «Сделать под ключ» is present and never started |
| Задание: отправка без почты преподавателя говорит причину | AC-14: «Отправить преподавателю…» without a teacher address gives the reason and «Указать почту» (to the subject page); no dialog, no email |
| Задание: окно «Отправить задание?» и отмена | AC-14: with a synthetic teacher address set through that way out the button stops at «Отправить задание?» (version, recipient, subject, attachment); «Отмена» sends nothing |
| Журнал: своя правка задания не пишется (ADR-0029) | after an add and an edit by hand «Журнал» (it runs after the draft row, so the page has the line «Решение задания» to show) has no «Добавлено задание» / «Изменён срок задания» / «Изменено задание»: by design the student's clicks are not in the agent journal |

- **Hub calls.** The only provider call of the pack is the draft (`homeworkflow/drafts.go`, `prompts.Build`). The rules
  (`scripts/llm/assignments/rules.ts`) match the data block `"assignment":{"title":"<title>"` for the first draft and the typed
  instruction itself for the regeneration (the instruction rides in `<student_instructions>`); titles and instructions must be
  free of `<>&"` that Go's JSON escapes. Replies are the strict `{"title","body"}` of `prompts.ParseDraft`.
- **Files.** «Файл .txt» does not open a dialog: it writes the export of the assignment inside the data dir. Only
  «Word (.docx)» asks where to save, so only it lands in `<ctl>/saved/`; `docx.ts` reads its `word/document.xml` (zip, no dependency).
- **Not covered, on purpose.** Extraction over an existing summary: the only UI path is the recording import (the recording pack);
  the material extraction is an agent tool. Google Calendar and the real send are out of E2E (ADR-0035). A delete's question
  «Работа остановится» needs a running draft and is not reachable with the instant fake.
- **Finding kept in a row.** After «Указать почту» the page of the subject shows the block «Преподаватели» several times (3 to 5 seen; DOM:
  `section.teachers` x3-5, all but the first with an open form). The row «отправка без почты…» asserts one block and is «сломано»; the next
  row goes on with the first form. Probable cause: `TeachersBlock` and `SubjectTabs` share `key={subject.id}` in
  `SubjectsPage.tsx` (a fragment with two equal keys).
- **Clock.** The app's clock is the real one; the pack computes dates from `new Date()`: deadlines three or more days ahead
  (never «завтра», also across midnight), yesterday and today 00:00 for the passed ones.

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
