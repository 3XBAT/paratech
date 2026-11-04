# TASK-377: E2E pack «Настройки и подключения» (`@settings`)

## Done
- Pack `paratech/e2e/specs/settings/` (hub, model, schedule, support): 10 rows, serial order hub < model < schedule (unit test checks file order and that every `requires` points to an earlier row).
  - Hub: address + run key typed in the UI, quota «Лимит» 400 000; revoked key shows the refusal («Hub не принял ключ…», card «Ключ не принят»); the run key is typed back by its own row («Restored», `requires` connect) so later packs keep a valid key.
  - Model: corrupted copy (one byte flipped) refused with a readable Russian SHA-256 text, nothing installed; good model installed with checksum equal to `catalog.json`; made default; speech component positively checked through the real binding (`SpeechService.Status` state=ready) plus the DOM.
  - Schedule: synthetic week built at run time (Матанализ, Литература, История, a lesson 2 min after the file is written), preview, «Применить» (added 3), re-import «Изменений нет», lesson on «Сегодня»; subjects from the schedule; the call link saved for «Матанализ» (not opened: `opened.jsonl` has no telemost entry).
- `scripts/fetch-model.ts` + `src/fixtures/model.ts`: size, SHA-256 and URL read from `catalog.json` at run time, `.part` + rename after verification, size cap, kinds `network` (= «не проверено») / `mismatch` / `catalog` (= «сломано»), cache `paratech/dist/cache/e2e`.
- Harness: `src/hub/keys.ts` (`issueKey`/`revokeKey`/`listKeys` with the Hub CLI), Hub DB URL exported to workers through `process.env` only (`EnvHubDatabaseUrl`, scrubbed, not in `state.json`).
- Layout follow-ups: console watch logs the URL (no query/fragment/userinfo) of failed requests; `steps/stubs.ts` uses `evalInPage`.

## Real run (this machine, 04.11, coordinator's own run, `--grep @settings --no-build`)
10 tests, 10 готово, 0 сломано, 0 не проверено, 27.8 s (first run with build and the model download: 1.3 min of tests). `npm test` in `paratech/e2e`: 340 pass.

| Функция | Итог |
|---|---|
| Hub: адрес сервера, ключ и остаток лимита | готово |
| Hub: отозванный ключ показывает отказ | готово |
| Hub: ключ прогона принят после проверки отзыва | готово |
| Модель: испорченная копия отклоняется | готово |
| Модель: импорт с проверкой SHA-256 | готово |
| Модель: по умолчанию | готово |
| Модуль расшифровки готов | готово |
| Расписание: импорт из файла с предпросмотром | готово |
| Расписание: предметы из расписания | готово |
| Расписание: ссылка на пару | готово |

## Findings of the pack (no row red)
1. **UX: the model-import answer is off screen.** The ⋯ menu of «Base Q5» is far down the catalog, the refusal «Контрольная сумма SHA-256 не совпадает…» / «Модель установлена: Base Q5.» appears at the top, outside the visible window: a student can click and see nothing. Recorded as a note on the rows; screenshots `report/screenshots/*modeli-okno-posle-otkaza*` and `*posle-ustanovki*`. Candidate for a UI task.
2. UX (minor): after a rejected key the key chip still says «Ключ задан» next to the red refusal.
3. The ICS importer ignores `LOCATION` and `DESCRIPTION` except «Преподаватель:» (`icsschedule/doc.go`), so the synthetic call link in the event text does not reach the app (by design). The pack saves it through «Сегодня» → «Добавить ссылку» for the agent and popup packs. The task text says `IsCallHost`; the function is `ParseCallLink` (`domain/schedule/values/calllink.go`).
4. `SchedulePage` has no lesson list after «Применить» (only the result line and the hint), so the AC wording «Сегодня»/«Расписание» is checked on «Сегодня» and by the result line + re-import.

## Decisions to confirm
- Files outside the task's «Allowed to change» list, needed for the revoked key and the two layout follow-ups: `src/contract.ts`, `src/hub/{index,keys}.ts`, `src/sandbox/run.ts`, `src/report/scrub.ts`, `src/checks/{watch,evaluate,types}.ts`, `src/steps/stubs.ts`, `tsconfig.json` (include `scripts/`), `package.json` (script `fetch-model` only), tests. No dependency added.
- The generated lesson starts 2 min after the file is written (not the run start), so it starts during this pack; the popup pack (TASK-379) needs its own lesson. If the lesson falls after local midnight the «Сегодня» part is `test.skip` (row «не проверено»).
- «Импортировать модель» of the AC = the real menu items «Импортировать скачанный файл…» / «Импортировать файл модели…».
- AC-07 «по умолчанию» is covered for one installed model only (a second model would need another download).

## Follow-ups
- **Pack order in a full suite:** `specs/smoke` runs last alphabetically and its check that `consumed.jsonl` is empty fails after any pack that used picks (layout, settings; reported by the tester in a full run: 29 passed, 1 failed (smoke)). Fix in `specs/smoke` (baseline at start) or run smoke first. Not done here (config/smoke out of scope).
- Layout lessons are replaced by the settings import in a full run («удалено 19»): the app's replace-in-window behaviour; later packs seed their own data.
- Layout invariants (ADR-0035 §5) are not run inside this pack (only in `@layout`).

## Changed existing tests
`run.test.ts` (saved/restored `EnvHubDatabaseUrl`, new assertions), `hub.integration.test.ts` (key issue/revoke block inside the existing test), additions in `layout.test.ts`, `steps.test.ts`, `layout-rules.test.ts`. No assertion removed or loosened.

## Review
reviewer (opus): changes requested (high: revoked-key row could leave the run key broken; medium: vacuous speech row, fetch error kinds, skipped «Сегодня», scope note) -> fixed, re-review approve (two lows fixed by the coordinator: poll timeout/message, cleanup `rmSync` guarded). security-auditor: no findings.
