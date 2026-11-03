# TASK-371 — owner refinements to TASK-368 (chat retry, bulk delete, call-link card as icons)

Frontend only; `src/popup` untouched. Uses `IconAction` of TASK-368.

## Done
- Chat stopped/problem lines: «Повторить» (continue/retry/restart/rerun), «Отправить ещё раз» (resend) -> retry glyph, «Продолжить» -> play glyph; tooltip = aria-label = the old words. The sentence names the way on («Можно повторить/продолжить»); the busy-refused line adapts to the icons actually drawn, its stop icon is «Остановить предыдущий запрос». Quota text no longer quotes a missing word. Schedule `partialFallback` reworded for the same reason.
- ConfirmCard/ScheduleCard "card could not be read": reload icon inline in the error line, text «Карточку не удалось загрузить. Ничего не выполнено. Можно повторить.» (specific core refusals keep their text; `genericError` of the binding unchanged).
- RecordingsPage «Удалить выбранные» -> trash `IconAction`, tooltip/aria-label «Удалить выбранные (N)», new variant `danger-solid` (red at rest so it does not read as the grey «Снять выбор» cross); confirmation dialog and busy guard unchanged. The only page with bulk delete of checked items.
- CallLinkParts: «Изменить» (pencil; «Изменить: <пара>» on Today, «Изменить ссылку» in the subject block), delete (red trash, «Удалить ссылку»; the old visible word was «Удалить», «Убрать ссылку» was the task's paraphrase), «Вернуть ссылку» (undo, keeps the focus id), failed-open «Повторить открытие ссылки» (retry; «Подключиться» is still on the card). Words kept: «Подключиться», «Добавить ссылку», «Сохранить», «Отмена», «Открыть «Предметы»» (sole way out of a refusal).
- WorkCard plan-editor row buttons moved from `IconButton` to `IconAction` («Поднять/Опустить/Убрать раздел N»), `disabledReason` for the remove that is refused; `IconAction` skips `aria-describedby` when the tooltip is inside the label.
- Tests: `iconaction368`, `iconbuttons368b`, `callLink364ui`, `words344` (no stop/problem text quotes a button word that is not visible), agent card tests updated.

## Kept as words
Card-form Save/Cancel/Skip, «Сохранить план», «Отменить всё», «Удалить все», «Развернуть/Свернуть все», each card's main action (offer buttons, «Да, выполнить», «Утвердить план», WorkCard/TriageCard primary «Повторить»/«Начать»), «Открыть задание/Открыть «Предметы»» refusal exits, Today heading «Повторить» of call links (pages/TodayParts.tsx, outside the file list), dialogs, popup.

## Decisions to confirm
- Chat retry/continue and the unreadable-card reload are icon-only (as the owner decided); the sentence now says «Можно повторить». If the owner prefers a word for the card reload or the failed-step line, flip it.
- Bulk-delete is an icon (as decided); the ux-expert would have preferred the word «Удалить (N)» since it is the bar's only action.
- Consolidation is partial: `IconButton` left src/agent; it remains in pages/SubjectsParts.tsx (two «+» tree adders, outside the file list, pinned by subjectTabs353) and `ActionButton` (`.ag-ib`: chat header/rows/message actions/RestartDialog, own 28px hover-reveal CSS, pinned by chatheader365) is not merged. Follow-up.

## Follow-ups
- Today-heading call-link «Повторить» is still a word.
- `ScheduleCard` «Отменить всё» is an icon (TASK-368 chose that) though the owner's rule says bulk «Отменить всё» keeps a word; flip it if wanted. (The text that referenced it is reworded.)
- Pressed retry has no spinner (icons go grey until the next state).
- Reviewer lows: busyBoth wording «Его можно остановить или отправить этот ещё раз» switches object (suggest «Можно остановить его или отправить этот ещё раз»); `removeBlockedReason` says «такой части отчёта» (name the part); missing space in IconAction.tsx className expression (cosmetic).
- Plan-row icons are 32px; check visually whether `size="sm"` fits the dense row. ✕ means remove/clear/cancel in three places.

## Changed existing tests
`iconaction368`, `iconbuttons368b`, `callLink364ui` (word checks -> name/glyph/tooltip checks; word-button lists lose the converted buttons), `agentstoprerun`, `agent`, `words344`, `agentwave10cards324(b)` (new sentences; `>Повторить<` -> `aria-label="Повторить"`). Nothing deleted or skipped.

## Review
Reviewer (opus): requested small changes (text naming a missing word, no hint beside icon-only retry, task notes) -> fixed; re-review approve (3 lows above). ux-expert: «доработать» (quota text, stop lines, card reload, bulk trash looks like cross) -> fixed; not re-run. security-auditor: no findings (confirmations and busy guards kept).
