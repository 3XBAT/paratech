# BUG-20261108-agent-cards: tool cards vanish after the reply (D1), the answer card shows no answer (D2)

Found by the agent E2E pack (TASK-380). No tasks.json entry.

## Done
- **D1 (high), core.** `input/agent` Driver remembers the TOOL cards of the request being driven (answer, confirm, action line, offers; `shown.go`) and puts them before the step's own cards in every later update of that request. Step cards (plan, questions, reply, stopped) are not remembered, so a plan card still goes after «Да» and the reply stays last. One entry per thread (newest turn only; a new turn, Rerun or a new Ask starts empty; an older turn changes nothing), at most 64 cards, `reflect.DeepEqual` dedupe (a replayed step). Updates without status or thread (refused button, failed Ask), Progress and Compaction updates carry nothing. The panel keeps its replace semantics; no DTO or binding change; `agentturn` unchanged.
- **D1, panel.** Tests only (`agentdriverupdates.test.mjs`): the second update of a turn replaces the cards, a DONE request renders answer + confirm + «Отменить» + reply, switching chats and snapshots keep the cards. No loss found on the panel side.
- **D2 (medium).** `AnswerCard.tsx` + `answerView.ts`: one read per answerId feeds the claims and the sources in the same render. Claims are React text nodes (one `<p>` each), «Не из материалов пар:» marker for claims not from the lessons, «В материалах не нашлось: …» line, empty states, «Загрузка ответа…», failed state with a «Повторить» icon, claims over 600 chars clamp (cut at a word) with «Показать полностью» / «Свернуть», `aria-live`, source hover now visible (`--fill`).
- ux-expert fixes applied: retry on failure, hover, empty-answer text and the sources-only note, word-boundary clamp, aria-live.

## Real E2E (heavy.sh, Docker up, `--project settings --project recording --project agent`, exe built before with build-alpha.ps1 -E2E, run with `--no-build`)
Before (TASK-380): 35 tests, 30 готово, 5 сломано. After: 35 tests, 32 готово, 3 сломано, 4.9 min; no assertion relaxed.

| Строка | До | После |
|---|---|---|
| Агент: карточки инструментов остаются после ответа модели (D1) | СЛОМАНО | готово |
| Агент: что было на паре — текст ответа в карточке (D2) | СЛОМАНО | готово |
| Агент: что было на паре — карточка с источником | готово | готово |
| Агент: действие класса C только через подтверждение | готово | готово |
| Агент: добавь предмет по последней записи | готово | готово |
| Агент: после оборванного ответа можно писать дальше (D4) | СЛОМАНО | СЛОМАНО (ожидаемо) |
| Агент: «Отменить» обновляет открытое дерево предметов (D5) | СЛОМАНО | СЛОМАНО (ожидаемо) |
| Агент: второй вопрос, пока идёт инструмент (D3) | СЛОМАНО | СЛОМАНО (ожидаемо) |
| Остальные 29 строк settings/recording/agent | готово | готово |

## E2E harness problem found (master, not fixed here)
`npm run e2e` WITHOUT `--no-build` fails on current master: the harness starts the scripted Hub from `paratech/dist/e2e/hub.exe`, then `build-alpha.ps1 -E2E` (since TASK-385) removes a `hub.exe` "of an earlier -Beta build" (`build-alpha.ps1:652-655`) and gets «Access denied» because the Hub runs from it. Worked around by building the exe first (`build-alpha.ps1 -E2E`, no Hub running) and running with `--no-build`. Needs a fix (e2e Hub exe elsewhere, or build before the Hub starts).

## Decisions to confirm
1. Toggle «Показать полностью» is a word button (like the plan card's «Ещё N шагов»); the owner's icon-only rule for small secondary actions may want a chevron instead.
2. No `[n]` markers linking claims to sources (the sources list has no visible numbers); quotes are not shown on the card.
3. Card order: tool cards (answer, confirm, action line) come BEFORE the model's reply. ux-expert suggests evidence -> reply -> action (confirm/«Отменить» last, near the composer). Left as is: the task asks that cards stay, and the order is one place to change in the Driver (`update`).
4. Possible duplicate content: the card now holds the claims and the model's reply usually retells them. A prompt change (reply with one line after an `answer` card) is Hub-side: owner/architect decision; check on a real «что было на паре» run.

## Follow-ups
- Tool cards are held in memory per session: after an app restart a finished request's answer/confirm cards are not restored (`Current` returns only plan/questions). Persisting them is a separate task.
- «Стоп» publishes its stopped update from its own goroutine; in a rare interleaving it can land after the loop's update built before the tool card was stored and show the stopped request without that card until the next update (pre-existing ordering; the stop test ends the request by cancelling the context to keep order fixed).
- ADR-0021/0033 should state the contract: every update of a request carries its tool cards (replace rule in the panel).
- Claim cites (`cites[].source`) are not rendered; Cf (bidi) characters are not stripped from claims in `answer.plainText` (auditor: no spoofing path found, optional hardening).
- Whole-answer clamp (instead of per claim) and copy-of-answer (`replyCopyText` copies only the reply) from the ux review.
- D3, D4, D5 of TASK-380 stay open (separate round).

## Changed existing tests
- `input/agent/actions_test.go` (TestDriverInterruptKeepsTheOutcomeOfAnAction) and `assignment_test.go` (TestActionLineOfAnAddedAssignment): counted/collected cards across ALL updates; now assert on the LAST update (what the panel shows), which is stricter.
- `tests/agent_assignment_test.go`, `tests/agent_propose_test.go`: `ask` helpers returned the cards of all updates concatenated (carried cards would count twice); now the cards of the last update. The «once» checks are unchanged.
- `frontend/test/answersources296.test.mjs`: the removed self-loading `AnswerSources` component replaced by `AnswerCard`; loading text expectation; source guard covers the new files.
Nothing skipped or deleted.

## Review
reviewer: approve (no blocker/high); security-auditor: no findings (bidi characters noted); ux-expert: «доработать» -> items 1, 4, 5, 6(part), 11(part) fixed, the rest above.
