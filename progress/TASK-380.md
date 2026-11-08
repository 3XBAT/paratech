# TASK-380: E2E pack «Агент» (SCRIPTED) — and the app defects it found

## Done
- Pack `paratech/e2e/specs/agent/` (`@agent`, 18 rows, files 01..13, own project `pack("agent")` after `recording`, before `smoke`): real app + real Hub + the fake LLM playing the model. Asserts what the app sends to the model (`llm.requests()`: catalog v9 name set pinned in `scripts/llm/agent/rules.ts`, tool_result blocks, system prompt lines) and what the panel shows.
- Fake Messages API extended (small, additive; shared files, see below): `delayMs`, SSE `stream {chunks, chunkDelayMs, cutAfter}` (a cut = socket destroyed without `message_stop`), rule kind `cited` (ANSWER built from the first fragment of the Hub's request: chunk_id and a verbatim quote). A request with `stream:true` and a rule without `stream` gets the old JSON 200 (the Hub reads it as a unary answer), so every existing script is unchanged.
- Hub of SCRIPTED runs now has `HUB_PROVIDER_STREAM=true` (was false): the «Ответ оборван» scenario needs a real SSE cut. ADR-0035 §4 says false: needs an addendum (architect). CompleteStream/AgentStepStream only; other operations stay unary.

## Real run (this machine, 08.11, Docker up, heavy.sh, `--no-build --project=settings --project=recording --project=agent`, three identical runs)
35 tests: 30 готово, 5 сломано (all agent rows, on purpose), 5.0 min whole run, agent pack ~187 s (budget 5 min). No unscripted LLM call, no «hub rate limit».

| Функция | Итог |
|---|---|
| Ответ, копирование, повтор, таймер «Nс» | готово |
| Строка «Стоп» не перекрывает текст (две ширины панели, `rowSeen` обязателен) | готово (в раскладке было «не проверено») |
| Оборванный ответ сохраняет текст, «Ответ оборван. Можно повторить.», повтор заменяет блок | готово |
| После оборванного ответа можно писать дальше | СЛОМАНО (D4) |
| Второй вопрос во время первого | готово (очередь, см. ниже) |
| Начать заново отсюда / Форк | готово |
| Уплотнение контекста | готово (30 с, срабатывает на 10-м вопросе) |
| Названия записей доходят до модели | готово |
| Ссылка на пару; ссылка, когда её нет (нет перехода в «Расписание», правило промпта в system) | готово |
| Что было на паре: карточка с источником, источник открывает страницу пары | готово |
| Что было на паре: текст ответа в карточке | СЛОМАНО (D2) |
| Сбой инструмента без выдуманной причины (`{"error":"failed"}`, `unknown_id`, cannot_do) | готово |
| Добавь предмет по последней записи (+ «Отменить») | готово |
| «Отменить» обновляет открытое дерево предметов | СЛОМАНО (D5) |
| Действие класса C только через подтверждение (AC-18, «Да» не нажимается) | готово |
| Второй вопрос, пока идёт инструмент | СЛОМАНО (D3) |
| Карточки инструментов остаются после ответа модели | СЛОМАНО (D1) |

## Real app defects found (rows stay red, nothing relaxed; screenshots in paratech/dist/e2e-run/report/screenshots)
- **D1 (high).** Cards made by tools (answer card with sources, confirm card «Нужно подтверждение · Удаление записи», action line with «Отменить») disappear when the model's final `reply` arrives: `agentturn.stepOf` publishes only that step's cards and `agentState.applyUpdateEntries` replaces all cards of the request by the latest update. After the reply the student cannot confirm, open a source or undo. Shots `001/002-agent-kartochki-*-otveta-modeli.png`. Checked by the coordinator on the screenshots and in the code.
- **D2 (medium).** The answer card shows no answer text, only «Ответ с источниками подготовлен.» and sources (`agentView.ts` answerReady; claims never rendered; TASK-296 follow-up nobody did). The planner gets only counters (ADR-0022), so the student never reads the answer.
- **D3 (low).** «Отправить ещё раз» after a busy refusal leaves the refused request in the thread (question twice) with the stale line «Предыдущий запрос ещё выполняется…» although both are answered.
- **D4 (candidate, product question).** After a provider-side cut the panel shows «Нет связи с paratech» and disables the field until «Повторить» works, although the Hub answered (BUG-20261104 mapped this to offline by design; the dead end is the issue).
- **D5 (low).** After «Отменить» of a created subject the open «Предметы» tree keeps the subject until the section is re-entered.

## Decisions to confirm
1. **Second question while the first runs.** Task text/BUG-20261104 say the core refuses it as busy; in reality during the Hub step it is QUEUED («ждёт предыдущий запрос», sent automatically when the first ends), during a tool run it is REFUSED («Предыдущий запрос ещё выполняется…»). Rows follow the app. Owner: one behaviour, or accept two?
2. **HUB_PROVIDER_STREAM=true in SCRIPTED** (above): ADR-0035 §4 addendum.
3. **Scope beyond the draft's «Allowed to change»**: `src/fakellm/*`, `src/hub/env.ts`, `playwright.config.ts` (one `pack("agent")` line), `README.md`, tests `fakellm*.test.ts`, `hub-harness.test.ts`, `steps.test.ts`, `hub.integration.test.ts`. TASK-381 adds its own pack line in the same file: keep both on merge.
4. AC «the lesson moves under the new subject» is not covered: `create_subject` only creates the subject (noted in the spec and README). The task text says `list_recordings` and «is_error with the real reason»: catalog v9 has no such tool (recordings come from `find_lessons`) and ADR-0022 gives error enums only (`{"error":"failed"}`), so the rows assert those.

## Not covered
Model wording (LIVE, TASK-382); «Да, удалить» and the real deletion (never pressed); a claim cited from a transcript/lecture-text fragment; `show_plan`/plan approval; `offer_button` (catalog 10): the link reply offers nothing to open in v9 (the row walks «Сегодня» → «Подключиться» → `opened.jsonl` by hand).

## Follow-ups
- Fix D1..D5 (D1 first); then remove the 12-25 s `delayMs` holds of rows 08/10/11/13 (they exist to keep tool cards on screen) and save ~60 s.
- Reviewer lows left: 10-subject undoTree `finally` swallows «request ended» even when the main step passed; leaves the subject if the test fails before «Отменить»; one-shot tree read without poll; the undo check does not assert the other subjects remain. `delayMs + chunks*chunkDelayMs` can hold a response ~1060 s (bounded by Hub 5-minute timeout and test timeouts): optional cap.
- docs: ADR-0035 §4 and TASK-374 draft say `HUB_PROVIDER_STREAM=false`; ADR-0026 lines 7 and 60 «AgentStep stays unary» are outdated.
- Hub rate limit of the E2E key (30/min, burst 10) is only avoided by spacing; the compaction row spaces its answers with `delayMs`.
- `docs/process/e2e.md` should mention the agent pack and the `--project=` filter (not edited: docs/process).

## Changed existing tests
`fakellm.test.ts` («streaming, non-JSON and oversized...»: `stream:true` is now a JSON 200, the non-JSON 400 and oversized 413 parts kept), `hub-harness.test.ts` (expected `HUB_PROVIDER_STREAM` true), `steps.test.ts` (second half of «an unknown endpoint and a stream request...» now uses a malformed body, because a stream request is no longer unscripted), `hub.integration.test.ts` (message reworded). Nothing skipped or deleted.

## Review
reviewer (opus): changes requested (high: a found defect downgraded to a note; medium: swallowed `.catch`, AC part, scope/ADR drift) -> fixed (two new red rows, the `.catch` removed, AC recorded); re-review: approve, lows above. security-auditor: no findings (loopback + Host guard cover the new paths, all new numbers capped, no ReDoS, nothing real is opened, «Да» of the delete card never pressed, no dependency change). Unit tests: 445, 444 pass, 1 skipped (Docker Hub test by design with `PARATECH_E2E_UNIT_ONLY=1`; it passed without it in step 1).
