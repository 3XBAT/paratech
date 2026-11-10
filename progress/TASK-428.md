# TASK-428 — scrollbars on activity, gap under «Ссылка на пару», status column of «Пары дня»

## Done
- `src/ui/scrollActivity.ts` (new): `installScrollActivity(doc)` — class `scroll-active` on the scrolled element (scroll, capture, passive), removed 1000 ms after the last activity (timer per element); pointer over the scrollbar strip (vertical/horizontal, borders excluded, once per frame) keeps it on, also while the pointer rests there; a thumb drag holds it until pointerup/pointercancel (or a move with no button pressed); pointer leaving the window from the bar starts the timer; inline boxes (client size 0) are skipped. Installed in `src/main.tsx` and `src/popup/main.tsx`. Tests: `test/scrollActivity428.test.mjs` (Node fake timers, fake DOM and geometry).
- `src/ui/scrollbar.css`: thumb transparent at rest, TASK-363 colors only under `.scroll-active` (hover/active as before); forced colors: unconditional block, thumb always visible (CanvasText, Highlight on hover/drag, `forced-color-adjust: none`); `.template` (horizontal-only CSV example) keeps a always-visible thumb because a wheel does not scroll sideways.
- `scrollbar-gutter: stable` on `.sheet`, `.tree`, `.del-body`, popup `.texts`, `.ag-thread`, `.ag-plan-scroll`. Exemptions (listed in the test with reasons): `.template`, `.ag-input`, `.ag-chat-menu`, `.ag-mpop`, `.picker .list` (open at final size, a blank strip would make insets asymmetric), `.dialog` (UA overflow, `components.css` outside the task).
- «Ссылка на пару»: `.section.calllink { margin-bottom: 24px }` — one gap to the tabs in every state, wide and narrow.
- Supervisor addition (owner 2026-11-10): «Сегодня» → «Пары дня», status column now lines up in every row. The lessons list is one grid (`.today-list`, class added in `TodayParts.tsx`), rows are `subgrid`. Wide tracks: `calc(4.5rem + 16px) minmax(10rem, 1fr) minmax(0, 12rem) calc(11rem + 16px)` (the row's 16px padding counts into the edge tracks). The actions track is FIXED (not `auto`), so the status column does not move when a row's buttons change (link field opens, «Записать» appears, «Подключиться» goes busy); «Добавить ссылку» + «Записать» wrap into two lines inside it. The title has a 10rem floor, the status takes the shortfall. Narrow rules (above the 38rem container query) untouched. Test: `test/layoutfix376.test.mjs`.
- `tsconfig.test.json`: `src/ui/scrollActivity.ts` added to `files` (needed to import the module in tests; outside the allowed list, mechanical). `src/README.md`: `ui/` entry mentions the new file.

## Fade spike result
Not tried. The loop may not start the app/WebView2, so a repaint cannot be verified here, and the task forbids shipping an untested technique. The thumb hides instantly 1000 ms after the last activity. `prefers-reduced-motion` block is a placeholder for the fade. The owner may try `@property` (registered color transitioned on the scroller) or the `border-color: inherit` trick by hand.

## Screenshots
None (no app started). Layout shift is prevented by `scrollbar-gutter: stable` and covered by CSS guards only.

## Decisions to confirm
- Instant hide after 1000 ms instead of a fade (see above); UX expert: acceptable, optionally 1.2-1.5 s.
- Fixed 11rem actions track in the Today lesson row; a row with two buttons gets two lines (about 80 px high). Button widths were estimated by character count, not measured on screen.
- Gutter exempt on menus/pickers; kept on the main sheet, tree, agent thread, plan, popup texts.

## Follow-ups
- Owner on screen: Today at 38rem and at a page width just above 62rem (two columns: the left list is about 570 px, the status column only ~80-120 px, chips wrap) with a live recording chip; a row with a link field open; the `.template` bar on «Расписание»; high contrast hover/drag; agent menus symmetric.
- Modal `.dialog` has no stable gutter (UA overflow; `components.css` not in this task).
- Windows setting «always show scrollbars» (`HKCU\Control Panel\Accessibility\DynamicScrollbars`) is ignored (ux-expert): a Go + frontend follow-up.
- Programmatic scrolls (agent thread auto-scroll while streaming, scrollIntoView on search hits) flash the thumb for 1 s; fine, mention in QA.
- Optional: wider hover zone (14-16 px) for the scrollbar edge.

## Changed existing tests
- `layoutfix376.test.mjs` «wide lesson row»: pinned the old per-row `... minmax(0, 14rem) auto`; now pins the new four tracks on `.today-list` (stricter: no `auto`, title floor, status cap, fixed actions track).
- `scrollActivity428.test.mjs` is new; `windowscroll363` and `contrast` unchanged.

## Review
Reviewer (opus): changes requested (forced-colors hover/active, title column collapse, gutters on menus, test regex, inline boxes, fade record) -> fixed; re-review: approve (two lows, above). ux-expert (opus): «доработать» (moving status column, title squeeze, `.template` bar, menu gutters) -> fixed, not re-run. Security-auditor (opus): no findings (presentation only, no trust boundary).

## Security
No findings; no dependencies, no CI/lint config, no Go.
