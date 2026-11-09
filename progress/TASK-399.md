# TASK-399: agent panel — waiting row with withdraw, «Повторная попытка N из 3», a cut reply without a lock, offline retry, D3

## Done (frontend + E2E rows, no Go)
- Waiting row of a queued question (driven by the ticket of `Ask`, no longer by the `queuedBehind` guess): «ждёт предыдущий запрос», icon «Остановить предыдущий запрос» (only on the oldest waiting row of a chat) and the new icon «Отменить этот запрос» (`Withdraw`). The row is replaced by the update that carries its ticket (matched before `pendingLocal`; a ticketed entry is never taken by `pendingLocal`). A late `queued:true` turn-0 update only gives its ticket (and chat) to a sent entry without one, never becomes a failed Ask or an orphan.
- Withdraw: `withdrawn`/`not_found`/`invalid` -> the row goes and the text (with mentions) returns into an EMPTY field, focus in the field; with a busy field the entry stays as a quiet line «Запрос отменён. Текст можно скопировать.» (`Entry.withdrawn`, excluded from every request logic); `started` -> nothing (the update replaces the row); `failed` -> the row stays, «Не удалось отменить запрос. Можно попробовать ещё раз.». No chat known -> no icon (never the open chat: a wrong chat reads not_found).
- «Повторная попытка N из 3»: quiet muted line next to the timer (`update.retry`, copied by the progress branch, gone with the answer or stop). Not for `answer_question` (no progress hook in Go: TASK-397 follow-up).
- D4: `panelMode` locks the input and shows «Нет связи с paratech» only from `link.readOnly`; an offline stop re-reads the link. A cut reply keeps its text, one retry icon, field enabled. Offline cut line: «Ответ оборван: нет связи. Можно повторить, когда связь вернётся.»; failed: «Ответ оборван. Можно повторить.».
- Offline card: «Повторить» (`retry`) instead of «Продолжить». The offline-without-open-step case: Retry is refused by an update `invalid` with no status; the panel (per-request watch, `retryWatchStep`) swallows it and calls `Continue` once, no visible error; any other problem (budget...) is shown.
- D3: «Отправить ещё раз» discards the refused entry (question once, no stale «Предыдущий запрос ещё выполняется…»). Sixth question: «Не принят: уже ждут пять запросов. Можно отменить один из них или остановить текущий.» (resend returns when the queue is not full).
- E2E pack: 12-busy-tool (D3) now expects queued (no refusal, second goes by itself, once, after the first); 03-busy comment; 02-cut (both tests) scripts 4 cuts (first call + 3 automatic retries) then a whole reply, checks «Повторная попытка 1..3 из 3», banner/field after the final cut line.

## Real E2E (this machine, 09.11, heavy.sh, Docker up, `--no-build --project settings --project recording --project agent`, 7.0 min)
36 tests: 35 готово, 1 сломано (D5 «Отменить» обновляет дерево предметов, owner postponed). D3 (12-busy-tool) и D4 (02-cut «писать дальше») — готово; 02-cut with the automatic retries ~40 s each. Build was made before the last tiny commit (withdrawChat without the open-chat fallback; no effect on a known chat).

## Decisions to confirm
1. Withdraw note for a failure goes to the `notice` line under the thread (where button refusals go), not to the chat note.
2. Busy-field withdraw keeps the question as a cancelled line with copy instead of dropping the text (ux-expert); acceptance text said only "returns into the field if empty".
3. The automatic Continue after a refused Retry (offline without an open step) is hidden: the refusal is only recognised as the status-less `invalid` update of the same request.
4. Stop-previous icon only on the oldest waiting row; the later ones only withdraw.

## Follow-ups
- `answer_question` «Повторная попытка» needs a progress hook in Go (TASK-397).
- `AgentAskResult` has no chat: returning `thread` would remove the guess (Go).
- Not driven by a DOM: AgentPanel wiring (withdraw race, retry watch) is covered by pure functions plus source-pattern tests.
- FIFO of local entries relies on `pendingLocal` taking the oldest entry (existing); a swap is possible when two Asks race for the core lock without a ticket.
- `restartOutcome` for a withdrawn restart request stays `wait` (existing behaviour).
- ux-expert lows skipped: cause wording in the retry line, tooltip of disabled icons.

## Changed existing tests
`agent.test.mjs` (offline card button `retry`; the field is no longer locked by an offline stop), `agentsnapshot.test.mjs`, `agentstoprerun.test.mjs` (offline button id), `agentStreamBreak20261104.test.mjs` (queued row by ticket; retryWatch Map; offline cut wording). E2E: 02-cut, 12-busy-tool rewritten for the new behaviour (nothing relaxed), 03-busy comment only. Files beyond the list: `src/agentElapsed.ts` (no timer for a ticketed entry), `src/README.md`, `src/agent/README.md`, `paratech/e2e/*`.

## Security / review
security-auditor (opus): none found. reviewer (opus): 1 medium (withdraw guessed the chat from the open one) fixed; re-review of the UX fixes done; ux-expert: 3 must-fix (text lost on busy-field withdraw, withdraw loop with unknown chat, queue-full wording) and 2 notable (stop icon on every row, offline cut wording) fixed.
