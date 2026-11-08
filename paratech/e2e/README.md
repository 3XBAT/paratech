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
| `specs/<pack>` | scenarios; `specs/smoke` is the `@smoke` start check, `specs/layout` the `@layout` pack (two window sizes: `PARATECH_E2E_WINDOW=1024x640`, `docs/process/e2e.md`), `specs/settings` the `@settings` pack, `specs/recording` the `@recording` pack, `specs/agent` the `@agent` pack (below) |

**Order of the packs.** `playwright.config.ts` has one project per pack, in the order layout, settings, recording, agent, smoke (one
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

## Agent pack (`specs/agent`, `@agent`, TASK-380)

SCRIPTED: the real Hub with the fake provider playing the MODEL's side with scripted tool calls (`scripts/llm/agent/rules.ts`,
`replyRule(match, text, {delayMs, stream})` = a `reply` tool call; `chatSummaryRule`). The pack asserts what the APP sends to the
model (`llm.requests()`: the student's text is the last block, the tool names are those of catalog v9, `stream: true`) and what the
PANEL shows; the model's own wording is the LIVE set (TASK-382). A request carrying another tool set fails with the names that
differ (`catalogProblem`): the pack is written for `values.CatalogVersion` 9. Every row `requires` the Hub key and the schedule
rows of the settings pack; every scenario starts with «Новый чат» so the history sent to the model holds only what it asked.
Files run by name: `01-reply`, `02-cut`, `03-busy`, `04-restart`, `05-compact` (part A, the panel itself), `06-titles`, `07-link`,
`08-answer`, `09-failure`, `10-subject`, `11-confirm`, `12-busy-tool`, `13-cards-stay` (part B, the tools). Run it with the packs it needs:
`… run e2e -- --project=settings --project=recording --project=agent` (a lone `--project=agent` on a fresh sandbox is
«не проверено»). The rows of part B also `require` the recording rows (the two imported recordings «Человек в футляре · основная» and
«… · перезапуск», the deleted «… · отмена») and, for the link, the settings row «Расписание: ссылка на пару».

| Row | What it checks |
|---|---|
| Агент: ответ, копирование, повтор, таймер | a streamed `reply` after a 2.5 s delay: the timer «Nс» counts, «Остановить» is in the row, the text grows in several steps and only forward, `checkAgent` (the 04.11 overlap check) runs during the delay and while the text types and `rowSeen` must be true; at the end the text is the scripted one, the row, timer and draft are gone, «Копировать» (under the answer and under the question) writes to `opened.jsonl` (`clipboard`: the copy goes through the shell, not `navigator.clipboard`), «Повторить ответ» asks again; the console is clean |
| Агент: после оборванного ответа можно писать дальше | a cut reply (provider side, the Hub answered): right after the cut line there is no banner «Нет связи с paratech» and the request field is enabled and takes typing (no dead end until «Повторить»); then the retry replaces the block so the chat is clean. May fail: the panel maps a broken stream to «offline» (candidate defect, product question) |
| Агент: строка «Стоп» не перекрывает текст на двух ширинах панели | the same overlap check on the default and the widest panel while a delayed streamed reply types; `rowSeen` must be true |
| Агент: оборванный ответ сохраняет текст | a 700+ character streamed reply is cut mid-way (BUG-20261104): the printed text stays, «Ответ оборван. Можно повторить.», one «Повторить» icon, no «Повторить ответ», no stop row/timer; «Повторить» with a full second reply replaces the kept block |
| Агент: второй вопрос во время первого | while the first request is in its Hub step the second WAITS (the core holds one lock per Ask, `agentturn.Service.Ask`): «ждёт предыдущий запрос» with «Остановить предыдущий запрос», no timer, nothing sent to the model; after the first answer the label goes at once, the second is sent by itself, the requests reach the model in order, a third question works. (The busy refusal «Предыдущий запрос ещё выполняется…» is only for a first request outside the Hub step, e.g. running a tool: not covered in part A) |
| Агент: начать заново отсюда и форк | «Скрыть» puts the text into the field, the hidden requests leave the thread and the next provider request holds the edit and none of the hidden questions/answers; «Форк» opens «… (fork)» with the text in the field, sends nothing, notes «Открыт новый чат. Исходный «…» не изменился.» with «Открыть исходный чат» |
| Агент: уплотнение контекста | nine requests of ~6 KiB fill the 48 KiB window; the next Ask shows «Уплотнение контекста…» while the scripted `chat_summary` answer is delayed, and the next planner request carries the summary instead of the oldest pairs |
| Агент: названия записей доходят до модели (TASK-369) | «какие у меня записи?»: the model calls `find_lessons`; the NEXT request's `tool_result` holds both recordings of the recording pack (`title`, `subject_name` «Литература», `recording` = the id the page's `ActivityService.ListRecordings` returns, `transcribed`, `summary`), not the deleted «… · отмена»; the first request has the student's text last and the v9 catalog; the reply card shows the scripted text |
| Агент: ссылка на пару (TASK-364) | `list_subjects` result: `call_link` = the saved link only on «Матанализ»; the reply shows the link as plain text; the thread has no button, link or card that leads anywhere (catalog v9 has no `offer_button`); then, as the reply says, «Сегодня» → «Подключиться» writes `{kind:"call"}` with the link to `opened.jsonl` (nothing else is opened) and the subject page shows the host |
| Агент: ссылка на пару, когда её нет | the same for «Литература» (no `call_link` on it or on «История»); no navigation control in the thread (nothing named «Расписание»); the planner prompt of the request holds the TASK-364 lines of `system.md` v9 («No tool saves a link… never name «Расписание»…», «Расписание … holds no links», where the link lives); the subject page of «Литература» has the block «Ссылка на пару» with «Ссылки на пару нет» and «Добавить ссылку» |
| Агент: что было на паре — карточка с источником (TASK-366) | `answer_question`: the ANSWER request to the Hub has the question and non-empty `fragments` with the text of the lesson (`cited` rule); the planner gets only counters (no claim, no quote); the card «Ответ по материалам» lists a source; its button opens the lesson page of a recording of the pack on the tab of the place («Конспект») |
| Агент: что было на паре — текст ответа в карточке | the card shows the text of the answer. **Fails today**: the card says only «Ответ с источниками подготовлен.» and the sources (TASK-296 follow-up, claims are parsed but not rendered) |
| Агент: сбой инструмента — честно, без выдуманной причины | the Hub's ANSWER reader answers garbage twice (the Hub asks again once) → the model gets `is_error` `{"error":"failed"}` only, no cause; no answer card, no «материала нет» in the thread; `make_summary` with an id the model never saw → `{"error":"unknown_id"}`, nothing done; a `cannot_do` reply draws «Целиком не получится» with the two lists |
| Агент: добавь предмет по последней записи (TASK-369) | the model looks first (`find_lessons`, no question card), `create_subject` runs (class U): the model gets `{"changed":true,"done":true,"id"}`, the line «Действие выполнено» with «Отменить» appears, the subject is in the tree of «Предметы»; «Отменить» removes it (the tree is as before once the section is opened again) |
| Агент: «Отменить» обновляет открытое дерево предметов | the same flow with «Предметы» kept open: after «Отменить» in the panel the subject must leave the visible tree within 5 s without re-entering the section. May fail (low defect: the page reads the library only on entering). The AC «the lesson moves under the new subject» is not covered: `create_subject` moves no lesson (follow-up, a note of the row above) |
| Агент: действие класса C только через подтверждение (AC-18) | `find_lessons` then `propose_delete_recording` with the id from that result: the model gets `{"proposed":true,"waits_for":"student"}` (no `done`), the confirm card shows the recording with «Нет» / «Да»; the recording stays in «Записи» (list and ids unchanged); «Нет» → «Отклонено: ничего не выполнено.»; «Да» is never pressed |
| Агент: второй вопрос, пока идёт инструмент | `answer_question` with the ANSWER reader held 8 s: a second question is refused at once, «Предыдущий запрос ещё выполняется. Его можно остановить или отправить этот ещё раз.» (not sent to the model); after the first answer «Отправить ещё раз» sends it and it is answered. **Fails today** on the last step: the refused request stays in the thread (the question twice, the stale refusal line) |
| Агент: карточки инструментов остаются после ответа модели | the cards of `answer_question`, `propose_delete_recording` and `create_subject` are seen while the model's reply is held, and must be on screen after it. **Fails today**: the model's `reply` update replaces every card of the request, so the answer card, the confirm card and the action line vanish when the reply arrives |

- **Hub rate limit.** One key gets 30 calls a minute with a burst of 10 (`HUB_RATE_LIMIT_*`, defaults); a scenario with many calls
  (compaction) delays each scripted answer (`delayMs`) so the questions go slower than the bucket refills, never a fixed sleep.
- **Unmatched rule.** An unscripted call fails the scenario; a question that must NOT reach the model (the busy one) has no rule.
- **Part B scripting.** A rule matches the student's text for the first request of a turn and a part of the tool result for the
  next one (`AfterLessons`, `AfterSubjects`, `AfterAnswer`, `AfterWrite`, `AfterProposal`, `afterRefusal` in `scripts/llm/agent/rules.ts`;
  the last user message of a follow-up holds only the results). `callRule` is one tool call, `citedRule` the ANSWER reader,
  `badAnswerRule` an answer the Hub refuses. The ids a tool needs (a recording to delete) are read from the app itself
  (`recordingsOfApp`: `ActivityService.ListRecordings` through the page's Wails runtime), the same ids the tool result must show.
- **Held reply.** Rows that use a card of a tool (files 8, 10, 11, 13) hold the model's final `reply` (`delayMs` 5–25 s): see the
  last row of the table above for why the cards cannot be used after it.
- **Untrusted wording.** Texts of the model are only drawn and compared as they were scripted; what the pack judges about words is
  what the APP says (error enums, «Действие выполнено», «Отклонено: ничего не выполнено.», no invented cause).
- **Copy.** The panel's «Копировать» calls the shell's clipboard (`TranscriptService.CopyText`), which the E2E hooks record as
  `{kind:"clipboard"}` in `opened.jsonl`; `clipboardWrites(page)` only sees `navigator.clipboard`, which the panel does not use.

## Fake Messages API

`node:http` on `127.0.0.1`. The Hub runs with `HUB_PROVIDER_STREAM=true` (TASK-380; ADR-0035 section 4 said `false`), so
it calls the fake with `"stream": true` for `CompleteStream` (the ANSWER operation only) and `AgentStepStream`. Rules
`{kind: "tools"|"plain", match, reply, text?, usage?, delayMs?, stream?}`: a request takes the first remaining rule whose `match` is a
substring of the last user message (text and `tool_result` text); the rule is consumed. No rule: HTTP 500 `api_error`,
listed as unmatched («unscripted LLM call»).

- `delayMs` (integer 0..60000, any rule): the fake waits that long before the first byte (SSE too). The rule is chosen,
  consumed and recorded first, so `remaining()`/`requests()` already show the call while it is held. A client that gives
  up during the wait gets nothing written.
- `stream: {chunks?, chunkDelayMs?, cutAfter?}` (tools and plain rules): when the request has `"stream": true` the answer
  is `200 text/event-stream`, events in this order: `message_start` (empty `content`, usage input fields),
  per content block `content_block_start` / `content_block_delta`s / `content_block_stop` (plain: one text block of
  `text_delta`s; tools: the optional text block, then one `tool_use` block per call, started with `input: {}`, whose
  `input_json_delta.partial_json` pieces join to `JSON.stringify(input)`), `message_delta` (stop reason, cumulative usage),
  `message_stop`. `chunks` (1..200, default 8) pieces per block (by code points, never an empty piece, an empty text is one),
  `chunkDelayMs` (0..5000, default 0) between deltas, `cutAfter: k` destroys the socket right after the k-th delta (before the
  first for 0; before `message_stop` when there are fewer), without `message_stop` and without a clean end: the Hub reports a
  transient provider error ("the LLM provider failed", retryable) and a client with deltas already out does not retry. A
  request with `stream:true` and a rule WITHOUT `stream`, or a request with `stream:false`/none, gets the normal JSON 200
  (the Hub reads a JSON 200 as a unary answer), so every older script works unchanged.
- `{kind: "cited", match, claim, words?, missing?, usage?, delayMs?}` for the ANSWER operation: the fake reads the Hub's
  user message `{"question", "fragments": [{"chunk_id", "text"}]}`, cites the FIRST fragment with a quote of its first
  `words` (1..30, default 6) whitespace-separated words, copied verbatim, and answers (plain JSON 200, `end_turn`)
  `{"claims":[{"text":<claim>,"cites":[{"chunk_id":…,"quote":…}]}],"missing":<missing or []>}`. A request without a usable
  fragment is HTTP 500, listed as unmatched (`cited: no fragment in the request`), and the rule is NOT consumed.
- Control: `POST /__e2e/script` `{"rules": [...]}` (strictly validated, 1 MiB
cap; replaces the script and clears the recordings), `GET /__e2e/requests`, `GET /__e2e/unmatched`. Any other path
is HTTP 404 and is recorded as unmatched too («unknown endpoint METHOD path»), so a new provider endpoint the Hub
starts calling is not missed. Recordings hold request bodies only, never headers. A request whose `Host` is not
`127.0.0.1:<port>` or `localhost:<port>` gets HTTP 403 (DNS-rebinding guard); the Hub and the specs use the literal URL.

## Решения / ограничения

- The fake returns no thinking blocks: SCRIPTED checks the app side (tools, quota, flows), not the model's reasoning.
- Streaming is per rule (`stream`); without it a `stream:true` call is answered as JSON. The Hub streams only the ANSWER
  operation through `CompleteStream` (any other operation gets `invalid_argument`, "use Complete") and `AgentStepStream`.
  A `cited` rule never streams (the JSON fallback).
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
