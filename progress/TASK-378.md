# TASK-378: E2E pack «Запись → конспект → задание» with the real whisper-cli (closes TASK-025)

## Done
- Pack `paratech/e2e/specs/recording/` (`@recording`, 7 rows, order 01-import < 02-cancel < 03-restart < 04-privacy; every row `requires` the settings rows it needs):
  - import of the clip through «Импортировать запись» (lecture typed by «Дата и время»: Литература, yesterday 10:00): statuses «В очереди → Расшифровка → Конспект готов» with no restart; real `whisper-cli.exe` of `dist/e2e` seen by exe path with `-m ...base-q5_1-<id>.bin` (AC-08: model id derived from the command line = `base-q5_1`, plus the `transcription job enqueued` line of desktop.log);
  - «Расшифровка» contains 7 of 7 README words (≥ 2 asserted), «Конспект» shows the scripted text, the scripted assignment with the real due date (weekday + day + month + 20:00 built from the app's own tables) is in «Задания» and «Сроки на неделе»;
  - cancel, restart (AC-09), privacy (AC-06 scan over all 6 provider requests of the pack, with a positive control that finds the real clip, and a check that the transcript words reach the Hub).
- Fixture `fixtures/audio/chekhov-chelovek-v-futliare-s1-20-70s.mp3` (50 s, 399987 B, SHA-256 in its README): bytes of the LibriVox «Человек в футляре» section 1 (archive.org item `chelovek_2103_librivox`, Public Domain Mark 1.0, text by Chekhov, d. 1904), MPEG frames cut on frame boundaries, no tags. The README has source, readers, licence, offsets, hashes, 7 expected words.
- Smoke baseline: `consumed.jsonl` is counted at the start of the smoke test (fix of the 377 follow-up).
- Harness: one Playwright project per pack in a fixed order (layout, settings, recording, smoke); `restartApp` (hard stop of the sandbox app, restart on the same data dir and port, pid recorded in state.json, stopped by teardown/exit guard; spawned detached so it survives its Playwright worker); process lookup by exe path (`procs.ts`); report section «Скорость расшифровки»; `requires` accepts several rows joined by « + ».

## Real run (this machine, 08.11, Docker Desktop started for it and stopped after)
Full suite `run e2e -- --no-build`: 40 tests, 37 готово, 0 сломано, 3 не проверено (pre-existing layout skips: «Страница пары», «Разрешения», «Строка Стоп»), 2 min 4 s. The @recording rows alone take about 66 s (≤ 5 min).

| Функция | Итог |
|---|---|
| Запись: импорт и локальная расшифровка | готово |
| Запись: расшифровка содержит слова из речи | готово |
| Запись: конспект из ответа Hub | готово |
| Запись: задание со сроком в «Задания» и «Сегодня» | готово |
| Запись: отмена расшифровки | готово |
| Запись: перезапуск приложения во время расшифровки | готово |
| Запись: звук не уходит в Hub | готово |
| Запуск приложения (smoke, after the pack) | готово |

STT speed line (for information): 50 s of audio in 11.8 s (x4.2 of real time), model base-q5_1, Intel(R) Core(TM) i5-10300H CPU @ 2.50GHz (CPU only, 4 threads, VAD on). Transcript starts: «в проекте, пожалуйста посетите сайт librivox.org. На самом краю в Селомероносецкого, старая старосты про кофе, расположились на начале к запоздахшей охотнике. Их было только двое, ветеринарный врач Иван Иванович и учител гимнасий Гуркин…» (names are mis-heard, the words used by the check are right).

## TASK-025
Set to done by the coordinator on the supervisor's instruction: a real whisper-cli of the exe folder ran on Windows with the real model and a real recording, the JSON result was parsed (transcript and words in the UI), a running job was cancelled (the process disappeared, `outcome=cancelled`), a job survived a hard stop of the app, the speed was recorded. Caveat: cancel was done by deleting the recording (see below), not by a «cancel» button.

## Decisions to confirm
1. **AC deviation, product finding.** The app has no UI to cancel a running STT job («Остановить» exists only for a recording being captured; «Фоновая обработка» lists jobs without a recording). The only way to cancel is «⋯ → Удалить» on the row during recognition. The spec tests that (whisper process gone, row gone, `job attempt finished type=stt outcome=cancelled`, nothing sent to the Hub). Per the «no dead ends» rule this is a UX gap: candidate task «cancel a running recognition» (button on the row).
2. Fixture is MP3 (LibriVox native format; the draft said WAV/OGG; the app imports MP3).
3. Files outside the draft's allowed list, all needed for the AC (listed in the PR): `playwright.config.ts`, `src/app/{cdp,launch,procs,restart}.ts`, `src/contract.ts`, `src/sandbox/run.ts`, `src/report/{collect,model,render,rows}.ts`, `src/fixtures/{audio,lecture}.ts`, `specs/smoke/smoke.spec.ts`, `README.md`, `test/*`. No dependency added.
4. Lecture date: the pack types the lecture by «Дата и время» (yesterday 10:00) because «Пара из расписания» offers only today's lessons that have begun.
5. `docs/process/e2e.md` still says nothing of the project order, `requires` with « + » and the `--project` filter: update (I may not edit docs/process). A filter with `|` (`--grep "@settings|@recording"`) does NOT work through npm on Windows (cmd treats `|` as a pipe and only part of the pack runs): use `-- --project=settings --project=recording`. A lone `--grep @recording` on a fresh sandbox gives «не проверено (нужно: …)».

## Findings of the run
- Real app bugs: none; no row red.
- The `model=` field of the desktop.log enqueue line is an internal row id, not the catalog id; the model is proven by the whisper command line.
- Harness bug found and fixed: an app started by a Playwright worker dies with the worker on Windows (libuv job object), so the restarted app is spawned detached.
- Docker Desktop was not running at the start; the pack needs it (Hub Postgres).

## Follow-ups
- Product task: cancel button for a running STT job (above).
- Low (reviewer): a restarted app is no longer in a kill-on-close job, so a hard kill of the main runner leaves it running (docs line or next run's setup kills a sandbox exe by path); the deadline matcher assumes no DST change between lecture and deadline (not a thing in Russian zones); `modelFileInCommand` duplicates the stricter `modelIdFromCommand`.
- A stray `C:\Users\xvati\GolandProjects\dist\e2e-run\report\report.md` stub (outside the repo, from a `playwright test --list` of the implementer run from another cwd) can be deleted by hand.

## Changed existing tests
None loosened. `smoke.spec.ts`: only the `consumed.jsonl` check (baseline at start instead of empty file). Report tests extended for the speed section and several `requires`.

## Review
reviewer (opus): changes requested (high: posix `path` in Windows path checks would fail Linux CI `e2e-static`; medium: deadline check vacuous because the title contains «до », AC-08 model value not asserted, cancel assertions not tied to the STT job; lows) -> all fixed, re-review approve. security-auditor: no high or medium (the mp3 is 1914 clean MPEG frames, SHA-256 matches the README, no dependency change, process kills limited to pids that run the sandbox exe). Two wrong comments found by it were fixed.
