# TASK-271 — «Начать заново отсюда», variant «Форк»

## Done
- Domain (`thread/fork.go`): `ForkOf(src, n, past, id, now)` builds a new chat from the stored, not hidden requests 1…n−1 of a chat (`PastRequest` in, `Fork{Thread, Requests}` out). Copies keep their numbers, student texts, mentions, scope, frozen reply line (rebuilt with `Reply(entries)` when the source has none) and times; no steps, seals, plan, budget, actions, created or seen ids. `forkedFrom` = source; title = source title cut to ≤ 80 runes with « (fork)»; the summary is copied only if `Through < n` and `Through ≤` the last copy, dated at the fork. `ErrInvalidFork` for a bad n.
- Use case `agentchats.Fork(ctx, id, n)` → new current chat + the text of request n (without the upload line). The source is only read (its version does not change); allowed at any time, also while its request runs.
- sqlite: `ForkSource` (one read transaction, no 1024-row cap; entries read only for lines still NULL and only below n) and `SaveFork` (thread + copied turns + student rows in one transaction). Three new queries (`ListAgentForkTurns`, `ListAgentForkTexts`, `ListAgentRequestEntries`); no migration.
- Binding `AgentService.ForkChat(threadId, turn)` → `{problem, chat, text}`; problems invalid, not_found, busy, failed.
- Frontend: `forkChat` + strict `parseForkResult`; the TASK-249 dialog now has «Скрыть» / «Форк» / «Отмена» (the TASK-249 fork criteria, deferred to this task); after a fork the new chat opens with the message in its field (not sent), a quiet note «Открыт новый чат. Исходный «…» не изменился.» with an icon back to the source, the « (fork)» suffix is never cut by the ellipsis; typed text and an edit made after «Скрыть» are merged, never lost; refusals say why and offer «Новый чат» or the «Форк» button itself as the retry.

## Decisions to confirm
1. Hidden («Скрыть») requests are NOT copied into the fork (the task text says they stay hidden with the same numbers). Observably the same (Window, Fill, Earlier, compaction skip hidden requests), no leakage of hidden texts, and the fork's current request must be a non-hidden one. Numbering of the fork may have gaps. ADR-0028 should say it.
2. A copy is stored as DONE even if the source request was FAILED/CANCELLED/STOPPED (statuses of earlier requests are never shown); every copy has a non-NULL reply line, so TASK-250 retention never needs to touch a fork (copies have no calls/results rows).
3. An earlier request without any student text is skipped, not an error.
4. The binding also returns `busy` (id collision / version conflict), beyond the task's problem list; the page treats it as «повторить».
5. Mentions of the forked message come from the dialog target (the binding returns only `text`, as the task fixes).
6. Scope added: the «Форк» button/handling in the TASK-249 dialog (src/agent/*), the ForkChat row in docs/agent-parity.md, and `tests/agent_fork_sqlite_test.go` (adapter tests cannot import cases).
7. Changed texts: «Попробуйте ещё раз» was replaced by the impersonal «Можно повторить».

## Follow-ups
- `Rerun` of a fork's current request is not blocked by the source's live effects (copies have no journal lines under the new thread id); not reachable today, relevant when TASK-272/273 show live cards of ended requests: refuse Rerun on a copy or look at `ForkedFrom` (reviewer, low).
- `ListAgentForkTurns`/`ListAgentForkTexts` have no LIMIT (deliberate: a long chat keeps its first requests); consider a hard cap (reviewer, low).
- A refused send held behind «Вернуть текст сообщения» and edited by the student is dropped on «Форк» (`forkOpened`, `p.unsent`) (reviewer, low).
- After the dialog closed, `not_found`/`invalid` show the generic «Можно снова выбрать … → «Форк»» note although a retry cannot help; derive it from the failure's `way` (reviewer, low).
- The fork note can outlive its chat when `reconcileOpenChat` changes the open chat; clear it on `openId` change (reviewer, info).
- `ForkOf`: `Texts: [""]` is skipped but `["a",""]` is invalid; one doc line (reviewer, info).
- Forking from the first message gives an empty chat titled «<название> (fork)»; «Новый чат» then reuses it as the empty current chat (title stays); ux-expert: maybe hide «Форк» on message 1 (owner's call).
- UX owner's calls, not changed: the restart icon (counter-clockwise arrow) reads as «regenerate» in other chat apps, branching is usually behind edit/“…”; «Форк» is an anglicism, the dialog title is a yes/no question; «Скрыть» is the accent although «Форк» is the safe choice.
- Not changed: the tooltip «Начать заново отсюда» (owner's fixed name); ARCH-61 nit — clock/ID fakes inline in `tests/agent_fork_sqlite_test.go` instead of `testenv/`.
- Upload lines in copied student text stay (the planner sees that an attachment existed); the fork cannot use those uploads (provenance is empty) — re-attach needed.
- `desktop/internal/app` tests were not run (wiring unchanged, compiles through vet).

## Changed existing tests
- `agentrestart249.test.mjs`: "there is no «Форк»" now asserts the three buttons and their lines; the single-`aria-describedby` test is per-button now (two choices); the failed-«Форк» test follows the final texts (no separate «Повторить»).
- `iconaction368.test.mjs`: word-labelled buttons of `RestartDialog.tsx` now list `restartTexts.fork` and `forkNewChat`.
- `input/wails/agentsummary_test.go`: `oneID` (always th2) became a sequence, because NewChat and ForkChat both create chats; ForkChat joined the summary-leak checks.
- `thread/forkof_test.go` `TestForkOfInvalid`: the case "no text" (earlier request without text) is replaced by "only empty texts of n" — an earlier request without text is now skipped by design (covered by `TestForkOfSkipsRequestWithoutText`).

## Review
Reviewer (opus): approve, mediums (invalid refusal dead end, missing progress/QA) fixed, lows above; re-review of the fix commits: approve. ux-expert (opus): «доработать» — 4 must-fix (success signal and way back, edited «Скрыть» text lost, `invalid` dead end, duplicate retry) fixed; the rest above. Security-auditor (opus): no findings (copies hold only student text and frozen reply lines; empty provenance; hidden requests never read; bound SQL; no text/summary in logs).
