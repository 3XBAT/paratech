# TASK-376: layout invariants on every screen (E2E pack `layout`)

## Done
- `paratech/e2e/src/checks/`: `checkLayout(page, screen)`, `layoutSession` (all states of a screen, one failure listing every finding), `evalInPage` (CDP `Runtime.evaluate`, exempt from the page CSP), the page probe + pure `evaluateLayout` (unit-testable), console watch, agent-thread overlap check, synthetic ICS. Per state the spec prints and annotates what was examined (icon buttons, statuses, titles, texts), so a green row is not vacuous.
- Rules (all switchable in `opts`): `window-scroll` (scrollHeight <= innerHeight + 1 px), `overflow-x` (body and visible elements), `pane-scroll` (`main.sheet`, agent panel, thread do not scroll sideways), `clipped` (controls and text cut by `overflow:hidden`), `icon-button` (aria-label + tooltip; also `[role=tab|menuitem]` and glyph-only buttons), `status-quiet` (`[data-status]`: no fill, border, shadow, outline), `raw-text` (`undefined|NaN|[object|Error:|TypeError` and backend error forms, also in title/aria-label/placeholder/tooltips; `data-raw-ok` skips a container), `title-width` (< 96 px and wrapped/clipped, or > 4 lines under 160 px), `truncated` (a cut title needs a tooltip), `console` (console errors and page errors since the last check, with url:line), `agent-overlap`.
- `specs/layout/` (`@layout`): seeds synthetic data through the UI (ICS import, subjects from the schedule, 2 subjects, 2 assignments, long names), then one report row per screen and size «Раскладка · <экран> · <размер>»: Сегодня, Записи, Предметы (tree + each tab of two subjects), Страница пары, Задания (list + page), Расписание, Журнал, every Настройки item, База знаний, Билеты, Помощник (panel default / widest over every list screen), строка «Стоп», Всплывающее окно (DOM only), Консоль при посеве.
- Frontend: `StatusChip` has `data-status="<kind>"` (no visual change); tests updated (`status367`, `recordingrow`).
- Docs: `docs/process/e2e.md` (layout pack, both sizes), `paratech/e2e/README.md`.

## Real run (this machine, 04.11, E2E exe with the data-status build)
Default size 1010x683 CSS px (22 tests): 14 готово, 5 сломано, 3 не проверено. 1024x640 (inner 1010x603): 13 готово, 6 сломано, 3 не проверено. No whole-window scroll and no horizontal overflow on any screen at either size. `[data-status]` was found (Сегодня 2, Задания 2, Подключения 4, Модели речи 20) and quiet everywhere.

| Экран | обычное окно | 1024x640 |
|---|---|---|
| Сегодня | сломано | сломано |
| Записи, Журнал, Настройки (Основные, Подключения, Профиль, Оформление, Модели речи, Диагностика), База знаний | готово | готово |
| Предметы (дерево, вкладки) | сломано | сломано |
| Задания | сломано | сломано |
| Расписание | готово | сломано (404 при старте, см. ниже) |
| Билеты | сломано | сломано |
| Помощник (самая широкая панель) | сломано | сломано |
| Всплывающее окно, Консоль при посеве | готово | готово |
| Страница пары, Настройки · Разрешения, Помощник · строка «Стоп» | не проверено | не проверено |

## Defects found by the pack (not fixed here: separate tasks)
1. **Сегодня: кнопка «Добавить ссылку» обрезана слева** на 35 px (`.today-ac` in `.today-row.today-lesson`, text shows «авить ссылку») when the agent panel is open or wide. Window 1010 px. Real bug, the first to fix.
2. **Сегодня: описание пары (`.today-dlmeta .small.muted`) обрезано по высоте** (line clamp) with the panel wide, no tooltip with the full text.
3. **Предметы: имена в дереве (`button.ti > span.nm`) обрезаны многоточием без подсказки**; the toggles `button.tog` («Развернуть/Свернуть: …») have an aria-label but no title/tooltip (owner rule: small actions are icons with tooltips).
4. **Билеты: «Билеты к экзамену» в хлебных крошках сжато до 47-57 px («Биле…»)** with the agent panel widest, no tooltip: the same class as the 04.11 container-query bug.
5. **Задания: страница задания пишет в консоль `422 Unprocessable Entity` (`/wails/runtime`)** when it opens; find which binding call is refused.
6. **Consoles at app start (low, seen once at 1024x640 only):** `404 /favicon.ico` and `404 /wails/custom.js`.
7. **Помощник · строка «Стоп»** cannot be reached without a slow model answer (the fake LLM has no delay): «не проверено». A follow-up for the fake (delay per rule) makes the 04.11 overlap bug checkable for real; until then covered on an HTML fixture only.

## Decisions to confirm
- **Tooltip rule**: icon-only buttons need `aria-label` AND a tooltip = non-empty `title` OR a text element `.tip` / `.ia-tip` / `.ag-ib-tip` / `role=tooltip` (the rail and `IconAction` use their own tooltip span; adding `title` there would show two). The task text said `title`.
- **Two window sizes are two runs** (`PARATECH_E2E_WINDOW=1024x640`, one app start per run); the runner is unchanged. A single command running both sizes needs a runner change (follow-up, cli loop).
- **Rules beyond the AC** (`title-width`, `truncated`, `pane-scroll`, `clipped`, `agent-overlap`, backend error forms in `raw-text`): thresholds in `src/checks/types.ts`. They can turn rows «сломано» by heuristics: say if any is too strict.
- Visually hidden helpers, `aria-hidden`/`inert` subtrees and boxes clipped by a scrolling ancestor are excluded from `overflow-x`/`clipped` (documented in code).
- Skipped screens and the «Стоп» row are `test.skip` with a reason, so the report says «не проверено», not «готово».

## Follow-ups
- Disabled actions without a reason, tooltip that never shows, click target size, contrast of statuses, visible focus, statuses outside `StatusChip` (`.note`, `.result`), empty states before the seed, open menus and dialogs, error/degraded states (Hub unavailable), minimum window size (no MinWidth/MinHeight in `desktop/`), 150 % DPI (ux-expert, «позже»).
- Console errors of the app start are attributed to the first test that drains; the app has 404s on `favicon.ico` / `wails/custom.js`.
- Linux CI runs only the pure part of the checker tests (the real-DOM tests need an installed Edge and skip).
- `playwright test --list` with the repo config from another cwd writes a stray `report.md` outside the repo (the reporter resolves a relative path): do not run it; follow-up for TASK-375's reporter.
- The false console error of the first run was `page.waitForFunction` polling (eval blocked by the app CSP `default-src 'self'`): every spec helper must avoid string-eval polling (use `evalInPage` or Playwright locators).

- Re-review lows: `evalInPage` does not detach a dropped CDP session (`releaseEvalSession` unused); the clipped-text rule uses 1 px instead of `clipSlackPx` and may double-report with `truncated` (skip ellipsis/title-selector elements); `pane-scroll` counts opacity-0 tooltips and does not tell `overflow-x:hidden` from scrolling; counts are shown but no state fails on `visibleElements === 0`; the row «Настройки · остальные пункты» is green when there are no extra items; `src/steps/stubs.ts` still uses string `page.evaluate` (the same false CSP console error in other packs: switch to `evalInPage`).

## Changed existing tests
`status367` (expected chip markup gets `data-status`), `recordingrow` (same). Own earlier tests in `layout.test.ts`: attribute-based raw-text fixture now uses `data-*` (attributes are scanned now), an overflow fixture lost a fixed height.

## Review
reviewer (opus): changes requested (skipped screens shown as «готово», seeding without timeouts) -> fixed; ux-expert: «доработать» (clipped content, truncated titles without tooltip, raw text in tooltips, glyph buttons, vacuous overlap row) -> fixed, rest «позже»; security-auditor: no findings (test tool, sandbox only).
