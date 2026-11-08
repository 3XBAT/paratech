# TASK-391: UI of the local Hub: «Подключения» block and «Диагностика» state

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-389, TASK-390
- **PRD sections / AC:** ADR-0036 §3, §7; ADR-0034 (settings layout); owner UI rules (icons with tooltips for small
  actions, quiet statuses, one accent, no dead ends)
- **Агент:** только UI: вводится секрет; бета-инструмент
- **Branch:** ralph/<date>-task-391

## Goal
The owner enters the provider key once and sees whether the Hub runs and why not, with a next step for every case.

## Allowed to change
- `paratech/desktop/frontend/src/pages/ConnectionsPage.tsx`, `src/pages/DiagnosticsPage.tsx`, new
  `src/localHubView.ts`, `test/*`
- `docs/agent-parity.md` (one UI-only row)

## Out of scope
- Go code and `src/bindings/localhub.ts` (TASK-389, TASK-390).

## Acceptance criteria
- [ ] Shown only when `bindings/localhub.ts` says available (beta build); otherwise nothing renders, no error text.
- [ ] «Подключения» → block «Hub на этом компьютере (бета)»: «Ключ провайдера» and «Адрес базы данных» as write-only
      fields («задан / не задан», «Сохранить»), base URL, «Заголовок ключа» (`x-api-key` / `Bearer`), «Квота токенов»
      (empty = по умолчанию); after a change «Перезапустить Hub» (word button: the main action of that moment);
      «Выпустить ключ доступа» only when the app has no Hub key and the Hub runs.
- [ ] «Диагностика» → «Hub на этом компьютере»: quiet status (icon + coloured text, no fill) for every state and
      reason of ADR-0036 §7, each with its next step (link to «Подключения», «Проверить снова», «Перезапустить Hub»);
      «Открыть папку с логами» as an icon button with tooltip and aria-label; status polled every 3 s while the page is
      open.
- [ ] Russian, impersonal texts as text nodes; keyboard and aria; layout invariants of ADR-0035 §5.
- [ ] Tests: each state/reason → text and action; write-only fields never show a value; double click → one call;
      unavailable binding → nothing rendered.
- [ ] `npm --prefix paratech/desktop/frontend run test` and `run build`.
- [ ] Owner check `docs/qa/tasks/TASK-391.md`: Docker off → «включите Docker Desktop»; on → «Работает» by itself
      within a minute; close the app → the Hub still answers; open again → no second hub.exe in Task Manager; log in
      `%LOCALAPPDATA%\LearningAssistant\logs\hub.log`.
</content>
</invoke>
