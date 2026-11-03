# TASK-356 — меню «Настроек»: группа «Дополнительно», адрес сервера свёрнут

## Done
Frontend only (no Go, no new dependency), pure view state.
- `nav.ts`: `settingsMainPages` (Основные, Подключения, Профиль, Оформление), `settingsAdvancedPages` (Модели речи, Диагностика), `settingsAdvancedCaption`, `settingsPermissionsSlot = 1` (the place after «Основные» for «Разрешения», TASK-361; no item now). `SettingsTabs`: `role="group"` with the caption «Дополнительно» and a divider. The over-line «Изменения сохраняются сразу» is gone.
- «Подключения», Hub card: «Ключ доступа» visible; «Адрес сервера» in a `<details>` with a chevron (`hubAddressOpen`, `nextChosen`, `savedAddressNote`, `hubSetupHint` in `connectionsView.ts`); the setup hint sits above the key; «Сохранить адрес».
- Breadcrumbs (TASK-350 follow-up for the settings area): Connections sections and the Apple help show «Подключения / <раздел>» (first crumb = old back callback, focus returns to the card), no «Назад»; Apple help has «Вернуться к подключениям» at the bottom. Redundant over-lines removed on Connections overview, Models, Diagnostics, Оформление; «Профиль» keeps one muted line (it says what the data is for).

## Decisions to confirm
- The core ships NO default Hub address, so "non-default" of the AC is meaningless. The block opens when the saved address is empty, edited, has errors or the Hub is offline/not configured/failed; it is folded only when an address is saved and works. A manual choice wins and lasts the visit. If a built-in address ships later, restore the literal AC (open only when non-default or invalid).
- The fold's summary is «Адрес сервера», not «Дополнительно» (two «Дополнительно» on one screen sent students to the wrong place; ux-expert).
- Scope: also touched `AppleCalendarHelpPage.tsx`, `appleHelpView.ts`, `ModelsPage.tsx`, `DiagnosticsPage.tsx`, `diagnosticsView.ts`, `SettingsPage.tsx` (breadcrumb follow-ups of TASK-350 "settings area").
- Apple calendar name: «Календарь Apple» in the help title and its entry button; crumb = card title «Календарь на iPhone, iPad и Mac». `concept-final.html` still says «Apple Calendar» (not edited).
- «Хранится на этом компьютере» (Оформление) was removed as generic.

## Follow-ups
- Connections sections are local state, not routes: Alt+← leaves «Подключения» instead of closing a section.
- Over-lines remain on Journal, Schedule, Recordings and «К сегодняшнему дню» on «Сегодня» (outside the settings area).
- Page tests are source-text regexes (no DOM harness); the pure logic and Breadcrumbs/HelpView/SettingsTabs-level pieces are rendered or unit-tested.
- When both key and address are set no hint says who gives them (accepted).

## Changed existing tests
`nav.test.mjs` (grouped order, merge with 348 kept), `screens224.test.mjs` (over-line absent), `connections.test.mjs` (imports; hubHint/hubDesc texts; back button and `lead` removed), `help.test.mjs` (crumb instead of two «Назад»; new entry button text). New: `connections_address.test.mjs`, `breadcrumbs356.test.mjs`.

## Review
Reviewer: changes requested (empty address folded, chevron, hint wording) -> fixed; re-review: approve with mediums (QA text, crumb names) -> fixed in pass 4, not re-reviewed (small, tested). ux-expert: «доработать» twice, all must-fix items fixed. Security-auditor: not run — view-only, the address still goes through `saveHubUrl` unchanged.
