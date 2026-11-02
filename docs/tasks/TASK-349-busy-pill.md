# TASK-349: Background work is visible from every screen (busy pill in the rail)

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-348
- **PRD sections / AC:** ADR-0034 §1; audit А-0.13; PRD §15 (no notifications: status is in the window)
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-349

## Goal
A student on «Предметы» or «Задания» sees that a recording or processing is running and gets there in one click.

## Allowed to change
- `paratech/desktop/frontend/src/ui/Rail.tsx`, new `src/ui/BusyPill.tsx` and `src/busyView.ts`, `src/App.tsx` (mount
  only), `src/styles.css` (rail rules), tests

## Out of scope
- Tray changes, new bindings (use `activityApi.listRecordings` / the jobs list the recordings page already reads).

## Acceptance criteria
- [ ] Above «Журнал»: «● 12:40» (elapsed) while recording, else «⟳ N» while N recordings or jobs are in progress,
      else nothing (no empty slot that shifts the rail).
- [ ] Click opens «Записи»; tooltip and aria-label say what runs («Идёт запись 12:40», «В обработке: 2»).
- [ ] Polls only while the window is visible, at most every 3 s; one poll shared with nothing else is fine.
- [ ] prefers-reduced-motion: no blinking dot.
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
