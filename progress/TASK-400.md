# TASK-400: agent chat order, icon toggle, «Скрыть» text (D5 dropped)

Status in tasks.json stays **pending**: the D5 part (the open «Предметы» tree and subject page re-read after an agent action is done or undone) was DROPPED from this round by the owner (2026-11-08, "not needed now"). The E2E row D5 stays red as before. A first implementation of D5 existed on the branch (commits 382b2a2e..) and was reverted before the PR; the task text for D5 is unchanged and still open.

## Done
1. **Card order (Go, `input/agent`).** `Driver.update` returns cards in display order via `displayOrder` (`shown.go`): everything else (reply, plan, questions, answer, offer_button...), then the cards the student answers or undoes (`ConfirmCard`, `ActionLine`, `ScheduleSubjectsCard`, `DeleteAssignmentCard`), then `StoppedCard` last. Tests: `order_test.go`.
2. **Panel (frontend).** The reply draft (and answer drafts) and the kept text of a cut reply render just above the first "late" card (`isLateCard`, `firstLateCard` in `agentView.ts`), so the text no longer types under a confirm card and jumps above it. Late cards have identity keys (`cardKeys`: confirm target, action id, schedule token, an occurrence counter for repeats), so a confirm card is not remounted when the reply arrives. Tests: `agentLateCards20261108.test.mjs`.
3. **«Показать полностью»/«Свернуть»** in the answer card is a chevron `IconAction` with tooltip, `aria-label` and `aria-expanded`.
4. **«Скрыть» dialog text** (TASK-249, accepted by the owner): «Текст сообщения перейдёт в поле ввода. После отправки сообщения ниже пропадут из чата, и агент их забудет. Сделанные действия не отменяются.»; «Форк» unchanged.

## Changed existing tests
- `input/agent/assignment_test.go` `TestActionLineOfAnAddedAssignment` and `desktop/tests/agent_actions_test.go` `TestAgentActionLineStaysWithTheReplyOnSQLite`: reply first, then the action line.
- `agentfork271.test.mjs`, `agentrestart249.test.mjs`: the new hideHint text.
- `answercard.test.mjs`: the claim toggle is an icon with a label, not a word button.

## Decisions to confirm
- `offer_button` cards (retranscribe offer, «Начать запись») stay BEFORE the reply: the owner named only the confirm card and the «Отменить» line. The UX expert suggested moving them too (a button before the text that explains it). One line in `displayOrder` + `isLateCard` if the owner agrees.
- The hint reads ambiguously: «После отправки сообщения ниже пропадут…». UX expert's wording, same length: «После отправки из чата пропадут сообщения ниже…». Kept the owner-accepted text.

## Follow-ups
- Toggle tooltip opens above and covers the end of the claim; `place="right"` and `aria-controls` (UX low).
- Several long claims have identical toggle names for a screen reader.
- The plan card's «Показать ещё N шагов» is still a word button, the claim toggle is an icon: ask the owner.
- The drafts remount if a late card arrives while the reply is typing (rare).
- Reopened chats: cards live in memory only (existing); check they match the live order.
- D5 itself (above).

## Security
Auditor: no findings. The reorder only changes display order after the existing card validation; nothing is dropped or duplicated; model text stays React text nodes.
