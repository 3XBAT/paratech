# TASK-249 — «Начать заново отсюда» UI, variant «Скрыть»

## Done
- Agent panel: quiet action «Начать заново отсюда» under each of the student's earlier requests of the open chat (those `chatEarlier` returns, and live ones the core has numbered); warning dialog (`RestartDialog.tsx`) with «Скрыть» / «Отмена», focus on «Отмена», Esc, focus trap and return. «Скрыть» is disabled while the current request is not final, with a reason and a way-out button («Остановить» / «Отменить просьбу»).
- «Скрыть» puts the text and mentions in the input and sets a pending edit with a note and «Отменить»; the send carries `restartFrom` (open chat only). On success the hidden requests leave the view and `chatEarlier` reloads; on refusal the edit stays pending with the reason (busy field: «Вернуть текст сообщения»); resend never carries `restartFrom`. Chat switch / new chat / outside draft end the edit.
- Pure logic in `agentView.ts` / `agentState.ts`, thin components. New frontend tests in `test/agentrestart249.test.mjs`.
- Core (owner decision 2026-11-02, scope addition): `thread.RestartFrom` now sets `historyFrom = 0`, so the agent sees everything before the restart point again. Files: `domain/agent/thread/restart.go` + `restart_test.go`, `cases/agentchats/fill_test.go`, `adapters/sqlite/agent_restart_test.go` (tests only). Persistence needed no change (`Save` writes `history_from` with the new request and marks in one CAS transaction).

## Decisions to confirm
- Not literally `min(historyFrom, n)`: `historyFrom` is a lower bound of what is sent, so `min` would hide requests 1…n-1 for good. First version reset to 0 only when `n <= historyFrom`; the reviewer pointed out the other branch left a cut the chat does not show, so it is now always 0. Known downside: after an expired-request restart both the cancelled request and its restarted twin are sent as text pairs (duplicate text).
- «Форк» is NOT built: TASK-271 (ForkChat binding) is not merged, so the dialog has no «Форк» button. The TASK-249 acceptance criteria for the fork variant stay open; add them when TASK-271 lands (the supervisor decides whether to split the task).
- The card button of an expired / restart_required request is renamed «Начать заново» → «Запустить снова» (two near-identical buttons on one request).
- Dialog body text is the one fixed by the task. UX expert proposes «Текст сообщения перейдёт в поле ввода. После отправки сообщения ниже пропадут из чата, и агент их забудет. Сделанные действия не отменяются.» (needs owner OK).
- Client mirrors the core's `final` rule (`isFinalStatus`, `restartBlock`) only as a pre-check; the core still decides.

## Follow-ups
- Dim the edited bubble and the requests after it while the edit is pending; one quiet line «Скрыто сообщений: N» after a restart; hover area of the action; roving focus instead of a Tab stop per message (UX, low).
- Close the restart dialog when the panel closes / the chat switches (aria-modal covers the panel only) (reviewer, low).
- A refusal arriving while a new restart dialog is open restores the old edit behind it; switching chats after a busy-field refusal loses the refused text (reviewer, low).
- «Стоп» (card) vs «Остановить» (dialog): glossary pass.
- Opening a chat after an app restart: before the current request snapshot arrives «Скрыть» may look available; the core refusal then shows the busy text.
- `CompactionPlan` still ignores `HistoryFrom` (TASK-248 follow-up, TASK-268/269).
- ADR-0028 «Restart from an earlier message» does not mention `historyFrom` resetting; add a line (ADR not edited here).
- No DOM/keyboard tests exist (server-side markup + pure functions only): focus, Esc, Tab trap are untested.

## Changed existing tests
- `test/agent.test.mjs`: two card-button assertions «Начать заново» → «Запустить снова».
- Tests added in this task were adjusted during review (refusal texts, note text); no pre-existing Go test changed.

## Review
Reviewer: approve with changes (no blockers) → fixed (always historyFrom 0, visible action, refusal with a busy field, placeholder entry), re-review approve, 2 lows (above). UX expert: «доработать» 3 must-fix items fixed (name clash, note text, silent loss on refusal) plus cheap items; rest above. Security: no findings.
