# TASK-368 — word buttons in small places become icon buttons

Owner rule 2026-11-03: small/secondary places get icon buttons with a tooltip (hover and keyboard focus) and an aria-label carrying the old Russian words; words stay for main actions and dialog buttons. Frontend only.

## Done
- Shared `src/ui/IconAction.tsx` + `iconAction.css` (used by main window, agent panel; CSS also imported by the popup): 32px target (28px `sm` kept for none in practice), tooltip on hover (350 ms delay) and `:focus-visible` (immediate), flip/shift inside the clipping box, `display:none` until open (no phantom scroll), Esc hides it without swallowing the key, busy spinner + `aria-busy`, `disabledReason` keeps the button focusable (`aria-disabled`, click blocked, reason in tooltip and `aria-describedby`), `tipText`, `place`, `variant` danger, `data`/`onMouseDown` props, forced-colors and reduced-motion. `.ag-ib` (`ActionButton`, chat rows/header of TASK-365) is untouched.
- New icons: `open`, `undo`, `trash`, `chevup`, `chevl`, `copysel`, `insert`, `swap`, `paste`, `arrowup`, `arrowdown`, `folder`.
- Converted to icons (tooltip = aria-label = the old words; repeated rows name the object in the aria-label with a short tooltip):
  - Agent panel: plan/progress/waiting «Изменить» (pencil), «Остановить» (stop, also the work card and busy-refused line), «Отменить запрос» (x), action-line «Открыть» (chevron), «Отменить» (undo; red only when it stops a recording), `undo_action` offer, WorkCard «Добавить раздел», «Изменить план», «Скачать PDF», «Открыть PDF» (external open), «Открыть текст выступления», TurnkeyCards «Изменить» and the «Спросить по шагу» expander, ConfirmCard/mention-popup retry, RestartDialog «Отменить правку» (undo) and «Вернуть текст сообщения» (insert).
  - Screens: FileRows (open subject, go to Inbox, undo lines), DropZone/SubjectRemoval/AssignmentDoneParts close, TeachersBlock row «Указать»/«Изменить»/«Вернуть», Today summary rows (chevron), Recordings selection «Снять выбор», HomeworkPage version «Открыть», Journal row «Отменить», Summary tools «Скопировать» / «Открыть задание», LectureText «Скопировать» / «Сохранить как…», Transcript search prev/next, copy all / copy selection / save as, KnowledgeParts «Указать предмет» (folder), export «.md», OwnNotes (delete, up/down, replace, paste text, undo), PageViewer (back / next / close), ScheduleSubjectsReview done row and step buttons, Tickets «Сменить», cancel, undo, show/hide tickets, Models row «Удалить» and download «Отменить», Connections quota refresh.
- Tests: new `iconaction368.test.mjs`, `iconbuttons368b.test.mjs` (also pin the list of remaining word buttons in `src/agent`, `src/pages`, `src/popup`, `src/sourcePicker`, `src/ui`), helper `test/iconbtn.mjs`.

## Kept as words (ambiguous or main/dialog; reasons)
- Main action of a card/line/screen: offer buttons (one per card; destinations differ only by words), «Да, выполнить», «Не выполнять», confirm pairs («Да»/«Нет», schedule «Создать N предметов»/«Не создавать»), «Продолжить»/«Повторить»/«Отправить ещё раз» of stopped/problem lines, «Ответить», «Отправить», «Утвердить план», «Доработать», «Открыть в Word / PowerPoint», WorkSend, «Записать», «Добавить…», «Скачать», «Разложить», «Найти», «Импорт записи», subject-tree adders (two adjacent plus glyphs would be ambiguous).
- Sole way out of an error/refusal state (owner may want to flip this): Today load error, stop-problem «Повторить» / «Перейти к записям», Journal load error, Summary «Повторить» and «Сделать конспект заново» (spends quota), LectureText start/retry, ConfirmCard/ScheduleCard reload, FileRows ErrorLine and the «Обновить список» of the refused undo, ScheduleSubjectsReview check-failed retry, RecordingRow failed-step «Повторить» (TASK-347 chose the word on purpose), `RefusalButton`, «Вернуть»/«Обновить»/«Повторить» of RemovedNameLine, «Все задания», «Открыть подключения», `ChangeRefusal` «Открыть отправку».
- Inline form commit pairs (treated like dialog buttons): UnderstandingCard «Сохранить»/«Отмена», WorkCard «Сохранить план» and revise/edit «Отмена», «Пропустить вопросы» (the "no" of a yes/no pair), teacher/past-note/import forms.
- Bulk and quota/destructive-purpose: AgentCards «Отменить всё», DoneRow/ScheduleCard undo-all (icon), «Удалить выбранные» (the purpose of selection mode), SchedulePage «Развернуть все / Свернуть все» (single chevron would look like one group's toggle).
- Popup: all buttons stay words. It is one prompt window, every button answers the prompt («Начать запись / Не записывать / Закрыть», «Остановить / Скрыть», «Переключить / Оставить»); a header icon «Скрыть» next to a live recording could be taken for stop.
- Navigation/content, not actions: «Исходная расшифровка», «Открыть пару (дата)», chips, «Показать ещё/остальные», «Ещё фильтры», «Сбросить фильтры», «Фоновая обработка».
- ContextRing hint «Новый чат», `ag-plan-more` «Ещё N шагов».

## Decisions to confirm
- Error-state retry and inline form Save/Cancel stay words (ux-expert and reviewer asked for it); the owner's rule can be applied literally if he prefers icons there.
- `open` (arrow out of a box) is only for external opens; in-app opens use the chevron.
- Popup stays words.

## Follow-ups
- Three icon-button components coexist (`IconButton`, `ActionButton` `.ag-ib`, `IconAction`); fold into `IconAction`.
- «⋯» (`RowMenu`) still uses a native `title`, no focus tooltip.
- IconAction behaviour tests are source regex checks, not DOM runs.
- Optional: a visible reason for a disabled version-open on HomeworkPage while edits are unsaved; `data-open` with a keyboard-focused button turning busy (WebView2 should blur).
- Per-row generic labels left: OwnNotes trash/undo, Journal/Tickets undo.

## Changed existing tests
Found-by-text checks moved to aria-label/role+name in `agent`, `agentassignmentadded`, `agentwave10cards324(b)`, `agentmentionsui`, `agentreplydraft340`, `agentstoprerun`, `agentrestart249`, `work`, `turnkey`, `journal`, `homeworkJournal346`, `lessonRoute351`, `summary`, `libraryInbox`, `libraryUx339`, `scheduleSubjects312`, `rail348`, `quiet347`, `models`, `notelectures155`, `subjectTabs353`, `subjectremoval289`, `teachers238`, `messageactions365`, `today`, `lessonEntry352`. Where a button went back to a word the old expectation was restored. `tsconfig.test.json` adds `bindings/runtime.ts` and `pages/DropZone.tsx`.

## Review
Reviewer (opus): changes requested -> fixed -> re-review approve (nits fixed in round 4). ux-expert (opus): «доработать» (open glyph, swapped undo icons, own-notes glyphs, error retry, form buttons, «Пропустить», disabled reason) -> fixed; re-check «доработать» with one item (refused-undo refresh) + tooltips -> fixed in round 4, not re-run. security-auditor: no findings (frontend only; confirmations and busy guards kept).
