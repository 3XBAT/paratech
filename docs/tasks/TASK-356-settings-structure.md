# TASK-356: Settings menu — «Дополнительно» group, server address folded

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-348
- **PRD sections / AC:** ADR-0034 §4; audit А-0.9, 12.1, 12.2
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-356

## Goal
The student sees the settings they need first; support tools stay one click away below a divider.

## Allowed to change
- `src/nav.ts` (settings pages and labels), `src/pages/{SettingsTabs,ConnectionsPage}.tsx`, `src/connectionsView.ts`,
  `src/styles.css`, tests

## Out of scope
- «Разрешения» (TASK-361 adds the item after «Основные»), SMTP presets (later).

## Acceptance criteria
- [ ] Menu: «Основные, Подключения, Профиль, Оформление», a divider with the caption «Дополнительно», then «Модели речи,
      Диагностика»; the order constant leaves the slot after «Основные» for «Разрешения».
- [ ] The over-line «Изменения сохраняются сразу» is gone.
- [ ] «Подключения»: «Ключ доступа» stays visible with where to get it; «Адрес сервера» is under a folded «Дополнительно»
      that opens by itself when the address is set to a non-default value or is invalid.
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
