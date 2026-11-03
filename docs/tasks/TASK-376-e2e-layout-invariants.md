# TASK-376: Layout invariants on every screen

- **Agent:** frontend (sonnet)
- **Label:** test
- **Depends on:** TASK-375
- **PRD sections / AC:** ADR-0035 §5; ADR-0034 §1–4; TASK-367 (quiet statuses); owner: rail icons with tooltips
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-376

## Goal
Every screen of the real window is checked for the class of bugs the owner found by hand (window scrolled into an
empty strip, horizontal overflow, icon buttons without tooltip, loud statuses, raw error text).

## Allowed to change
- `paratech/e2e/src/checks/layout.ts`, `paratech/e2e/specs/layout/**`, `paratech/e2e/test/**`
- `paratech/desktop/frontend/src/ui/*` only to add a `data-status` attribute to the shared status component (no visual
  change), and its tests

## Out of scope
- Fixing layout bugs found: each becomes a separate task listed in progress.

## Acceptance criteria
- [ ] `checkLayout(page, screenName)` asserts: `scrollingElement.scrollHeight <= innerHeight`; `body.scrollWidth <=
      innerWidth` and no visible element's right edge past the viewport; every visible `button`/`a`/`[role=button]`
      without text has non-empty `aria-label` and `title`; `[data-status]` elements have a transparent background and
      no coloured border; no visible text matching `undefined|NaN|\[object|Error:|TypeError`; no console error or
      page error since the last check. A failure names the screen and the offending element.
- [ ] The spec visits with seeded data from the earlier packs: Сегодня, Записи, Предметы (tree, subject page tabs),
      lesson page tabs, Задания (list, one assignment page), Расписание, Журнал, every Настройки item, the agent panel
      open, the popup; at the default size and at `--e2e-window 1024x640` (second app start).
- [ ] Unit tests of the checker on static HTML fixtures (pass and fail cases) in `paratech/e2e/test`.
- [ ] Spec runs in the scripted run; current failures are reported as «сломано» rows, not hidden; `npm test` and
      `npm run test`/`build` of the frontend pass.

## Notes
The screen list follows ADR-0034 routes; a screen that does not exist yet is skipped with a note, not a failure.
