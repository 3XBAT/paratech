# BUG-20261104-agent-stream — long agent replies are no longer cut at 2 minutes; a broken reply keeps its text; timer and queue label

## Done
- Cause (desktop.log `agent step failed reason=offline err="hubclient: agent step: hub request timed out"`): `hubclient.Step` wrapped the whole AgentStepStream read in `RequestTimeout` (2 min), and the shared `http.Client` backstop capped the body read too.
- hubclient: a streamed agent step now has an idle watchdog (`StepIdleTimeout`, default 60 s between Hub messages; before the first message the old 2 min) and an overall cap (`StepTimeout`, default 10 min, never below `RequestTimeout`). Timeouts are `unavailable(ErrTimeout)` as before (reason offline, same key, «Повторить»/«Продолжить» replays the Hub's stored step); a caller cancel stays a plain ctx error. The unary fallback keeps its own 2 min. The http backstop is at least the step cap. Negative values and idle > cap are `ErrInvalidConfig`. Nothing to configure in the app (defaults apply).
- Panel: a step that fails after reply text was streamed ends its draft `failed` (new end value), so the last chunk waiting for the 100 ms throttle is shown instead of dropped. A user «Стоп» still cuts at once and drops.
- Frontend: the shown reply text now STAYS for every end of the request (it was erased for everything but a user «Стоп»). Line under it: «Ответ оборван. Можно повторить.» (offline, failed) with the existing retry icon, no second «Повторить»; a user stop keeps «Ответ остановлен».
- Frontend: elapsed timer next to the dots (and next to the stop icon while text types): «1с» … «59с», «1м 5с»; muted, tabular, `aria-hidden`, no live region; nothing under 1 s; restarts at 0 on «Продолжить»/«Повторить»; gone when the request ends.
- Frontend: label «ждёт предыдущий запрос» (muted static dots, no timer) with a stop icon «Остановить предыдущий запрос» for an unanswered request while a sibling of the same chat runs; screen reader hears «Запрос ждёт предыдущий».

## Hub findings (no Hub change, no restart)
- The Hub ends a step at `min(HUB_LLM_TIMEOUT, reservation left − 5 s)`: default 5 min, before the desktop 10 min cap. Reservation TTL default 10 min is consistent. No HTTP WriteTimeout. The Hub sends only reply-tool text deltas, no keepalive.
- If an operator sets `HUB_LLM_TIMEOUT` near 10 min, the desktop cap may fire first; the Hub still stores the step and «Продолжить» replays it.

## Decisions to confirm
- The core does NOT queue a second Ask behind a running request: `turns.Ask` answers `ErrTurnInProgress` at once, shown as the busy refusal («Предыдущий запрос ещё выполняется…» with stop and resend icons). The new «ждёт предыдущий запрос» row therefore appears only for the moment before that update. Either make the core really queue (product decision, separate task) or drop the label. Kept as the owner asked.
- Wording «Ответ оборван. Можно повторить.» (owner's «Ответ оборван.» plus the way on; UX expert asked for the way on to be in the sentence). UX expert also suggested «Ответ прерван» and a «0:05» timer format; kept the owner's formats.
- Idle 60 s: a step that writes reply text and then a very long non-reply tool argument could stay silent > 60 s (unlikely at the 4096-token planner cap); it ends offline and «Повторить» recovers it. Raise `DefaultStepIdleTimeout` if the owner sees false cuts; a Hub keepalive is the real fix.

## Follow-ups
- `AnswerQuestion` has the same total-cap shape (5 min over the whole stream read); give it the same idle watchdog if long answers hit it.
- The queued request cannot be withdrawn (no «Отменить этот запрос»); the kept partial reply has no copy icon (UX low).
- A FAILED older (not last) request with no stop reason shows the line without a retry button (pre-existing).
- The timer restarts from 0 if the panel is remounted mid-request.
- UX lows: the user-stop caption «Ответ остановлен» duplicates the stop line; `.ag-queued` wrapping on a very narrow panel is unchecked on screen.

## Changed existing tests
- `hubclient/answer_test.go` (`TestAnswerQuestionHasItsOwnTimeout`): `==` became `<`, the backstop now also covers the step cap.
- `input/agent/replydraft_test.go`: a failed step ends the draft `failed` (was `dropped`); test renamed `TestFailedStreamKeepsTheReplyDraft`.
- `app/agent_test.go`: `AgentDraftFailed == DraftFailed` added to the end-values guard.
- `frontend/test/agentreplydraft340.test.mjs`: the old «Ответ прервался…»/cleared-text assertions now assert the kept text and «Ответ оборван».
- `frontend/test/agentStreamBreak20261104.test.mjs` (new): wording, no «0с» markup, queued row.

## Security
security-auditor: no findings (watchdog releases its timer and contexts on every return, a fired watchdog is never re-armed, cause strings are fixed text, kept model text is a plain text node and UI state only, stop routing is limited to the same chat). Reviewer: approve; two mediums above (idle gap, core does not queue). ux-expert: «доработать» (wording without the way on, announcement of the queued row, two pulsing indicators, «0с») — fixed; the rest in follow-ups. Not re-reviewed after the UX round (text, markup and CSS only).
