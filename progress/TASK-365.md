# TASK-365 — значки действий под сообщениями чата и в шапке панели агента

Combined round with TASK-367 (one branch, one PR). Owner additions of 2026-11-03 are included.

## Done
- Under the student's message: quiet icon row «Копировать» + «Начать заново отсюда» (retry arrow, counter-clockwise) instead of the text link. Under the agent reply: «Копировать» + «Повторить ответ». Names = tooltip on hover and keyboard focus + aria-label; muted, brighter on hover/focus; 28px target, focus ring, forced-colors and reduced-motion rules. Restart (TASK-249 dialog) and rerun behave as before. New file `src/agent/MessageActions.tsx`.
- Copy: inline confirmation (icon becomes a check, tooltip «Скопировано», polite live region), no toast. Failure: error icon and «Не удалось скопировать. Выделите текст и нажмите Ctrl+C». Copy exists only under real messages (student text, `reply` cards, earlier requests); never under confirm/action/work/schedule/answer/stopped/problem cards or the typing draft. Clipboard goes through the existing Go binding `TranscriptService.CopyText` (the page clipboard needs focus/permission in WebView2); no Go change.
- One retry icon (`i-retry`), now counter-clockwise everywhere (also `OwnNotesParts`); the busy spinner of icon buttons spins in reverse for it. New icons `copy`, `pencil`, `archive`, `unarchive`, `error`, `help` (drawn by hand, no dependency).
- Owner addition, panel header: «Переименовать» (pencil), «В архив» (archive box; «Вернуть» for an archived chat), «Новый чат» (plus) are icon buttons with tooltip + aria-label on the title row; the pencil and archive are hidden while there is no open chat; 8px gap before the plus. The per-row «В архив»/«Вернуть» in the chat list are icon buttons too.
- Owner addition: the open chat's title no longer shows «идёт запрос»; in the chat list a non-open chat with a running request shows a quiet muted clock (title and screen-reader text «Идёт запрос»).
- Class names of the new icon-button family are `.ag-ib*`; the old `.ag-act` action-line card is untouched (a first draft collided with it; caught in review).

## Decisions to confirm
- «Начать заново отсюда» and «Повторить ответ» use the same retry glyph (owner's wish); tooltips tell them apart. ux-expert noted the familiar alternative (pencil «Изменить» under the student's message).
- The removed `restartTexts.actionHint` (native title) is replaced by the tooltip with the action name only; the TASK-249 dialog explains the consequence.
- Header order is pencil, archive, (gap) plus.

## Follow-ups
- «Копировать» under source-backed `answer` cards: not done (the card shows a fixed line and sources; the loaded text lives in `AnswerSources`; needs lifting state and a decision what to copy).
- Esc to dismiss tooltips (WCAG 1.4.13); message-row tooltip may be clipped when the first row is at the very top of the thread scroller (needs flip logic).
- Other fail chips (`statusPresentation`, Today problem chip) still use the `warn` glyph; only Banner/ResultLine/copy use `error`.
- Popup `.note` uses `border` while the main one uses a `box-shadow` ring (2px taller in the popup); copy-failure tooltip 2000 ms may be short for the longer text (3.5 s for the failed state); duplicate `.ag-chat-line` rule; running clock has both `title` and sr-only text.
- Owner rule «no word buttons in small/secondary places» — other violations found (not changed, to be checked whether they are main actions or dialog buttons; see TASK-368): `agent/AgentCards.tsx` (offer button ~97, «Открыть задание» ~502, «Отменить» ~510, secondary card buttons ~560, «Отменить всё» ~678, «Пропустить» ~393), `agent/WorkCard.tsx` (~108 «Добавить», ~338 «Остановить», toolbar ~541–556 open/save/PDF/talk-text, ~567/582 «Отмена»), `agent/TurnkeyCards.tsx` (~173 «Пропустить», ~278 «Изменить», ~349 expander), `agent/MentionParts.tsx` ~107 retry, `pages/FileRows.tsx` («Обновить» ~76/567, «Повторить» ~81, «Открыть предмет» ~135, «Открыть Входящие» ~201, row «Отменить» ~571), `pages/JournalParts.tsx` («Повторить» ~105, row «Отменить» ~163), `pages/AssignmentDoneParts.tsx` ~108 and `pages/DropZone.tsx` ~21 close buttons.
- Owner to check on screen: tooltips not clipped (top of the thread, panel right edge), the row stays quiet in both themes, the copy check mark and failure text in real WebView2, restart dialog opens from the new icon and returns focus.

## Changed existing tests
- `agentrestart249`: class `ag-restart` -> `ag-ib ag-ib-restart`; CSS test checks the icon-button rule and tooltip instead of the text-link `opacity`/`min-height`/`title`.
- `agentstoprerun`: «Запустить запрос заново» -> «Повторить ответ» (icon + tooltip markup).
- `agentchatsui`: open chat title has no «идёт запрос»; header buttons matched by `aria-label`; the pencil is absent (not disabled) without a chat.
- `turnkey`: the «no href» check ignores the fixed sprite reference `<use href="#i-…">` (new `help` icon).
- New: `messageactions365`, `chatheader365`.

## Review
Reviewer (opus): changes requested -> fixed; re-review of the fix commit: approve (lows above). ux-expert (opus): «доработать» (class collision, error vs warning icon, orange = accent, ask card, copy under answers, small items) -> fixed except the follow-ups above; not re-run. security-auditor (opus): no findings (copy only on user click through the existing capped binding; no HTML injection).
