# TASK-361: «Разрешения» in Settings and «Больше не спрашивать» on the first card

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-360, TASK-356
- **PRD sections / AC:** ADR-0034 §4, §5; owner О10 = б (2026-11-02)
- **Агент:** только UI: control of the agent itself
- **Branch:** ralph/<date>-task-361

## Goal
The student can stop being asked for a kind of send on its first card, see and change it in one place, and always
sees what was sent without asking.

## Allowed to change
- `src/agent/{ConfirmCard,WorkSend}.tsx`, `src/{agentConfirmView,sendWorkView}.ts`, new `src/pages/PermissionsPage.tsx`,
  new `src/permissionsView.ts`, `src/nav.ts` (settings page `permissions`), `src/App.tsx`, `src/pages/{SettingsTabs,
  SettingsPage}.tsx`, `src/styles.css`, tests

## Out of scope
- Core and bindings (TASK-358…360).

## Acceptance criteria
- [ ] A card with `offerDontAsk` shows the checkbox «Больше не спрашивать» under the buttons with a hint «Можно вернуть в
      Настройки → Разрешения»; it is sent only with «Да»; the buttons read «Да, отправить письмо» / «Не отправлять» and
      «Да, добавить в календарь» / «Не добавлять».
- [ ] A card confirmed by permission shows «Письмо отправлено без вопроса» (or «Срок добавлен в календарь без вопроса»)
      and the link «Спрашивать снова» (sets `ask`, result line in place).
- [ ] «Настройки → Разрешения» (after «Основные»): one row per kind — «Отправлять работы преподавателю», «Добавлять сроки
      в Google Календарь» — with «Спрашивать / Не спрашивать» and one line on what «Не спрашивать» means («paratech
      отправит письмо сразу, без карточки. Адрес и файл берутся из предмета, как на карточке.»); a footnote: «Агент не
      может менять эти настройки.».
- [ ] If TASK-280 is done, its choice «Обновлять событие при смене срока» moves to this page; otherwise TASK-280 puts it
      here (note in the progress file).
- [ ] Texts impersonal, Russian, text nodes only; keyboard and aria; dark theme.
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.
