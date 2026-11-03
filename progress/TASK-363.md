# TASK-363 — окно не прокручивается целиком; scrollbar темнее и тоньше

## Done
- **Cause (from the CSS, jsdom has no layout, not seen on screen):** `.vh` (visually hidden text, `position:absolute`, no offsets) and `.ag-vh` in the agent panel had no positioned ancestor inside their scroller. Their containing block was `.shell` (class `with-agent` is `position:relative`) or `.ag`, which do not clip. A hidden 1px box low in a long `.sheet` / `.ag-thread` kept its static position far below the window and stretched the document scroll area: the whole window scrolled into an empty strip. «Записи» has `.vh` in long lists (`RecordingsPage.tsx`), «Сегодня» through shared components. Not the cause: `.shell` height + padding (border-box is global), rail tooltips, toasts (fixed).
- **Fix:** `position: relative` on `.sheet`, `.ag-thread`, `.ag-plan-scroll` (no `overflow:hidden` on html/body/#root).
- **Scrollbar:** new `src/ui/scrollbar.css` (`::-webkit-scrollbar` only, 10px track / 6px thumb, hover and `:active` lighter-or-darker per token, forced-colors rule), imported by `main.tsx` and `popup/main.tsx` (the popup and the source picker had the classic scrollbar). All `scrollbar-width`/`scrollbar-color` removed from `agent.css` (Chromium ignores `::-webkit-scrollbar` when they are set). Tokens `--scroll`, `--scroll-hover` in all 3 theme blocks: light `#A4A49E`/`#7A7D83`, dark `#595C63`/`#70747C`.
- Tests: `test/windowscroll363.test.mjs` (CSS-text guards: positioned scrollers, no html/body/#root scrolling, no `scrollbar-width/-color` in any css/tsx, both entries import scrollbar.css); `test/contrast.test.mjs` (thumb vs bg/panel/raised: rest >= 2:1, hover >= 3:1, both themes).

## Decisions to confirm
- Thumb contrast at rest is 1.7–2.9:1, below the WCAG 1.4.11 3:1 for restyled controls (a 3:1 thumb would be as loud as icons and contradict «less attention-grabbing»); hover/drag reaches 3:1. First values (`#BDBDB7`/`#44464C`) were no darker than the Chromium default in light theme and too faint on cards in dark, so they were raised after the UX review.
- In the light theme the thumb gets DARKER on hover (a lighter thumb would vanish on the light panel); in dark it gets lighter.
- Arrow buttons of the scrollbar disappear (Chromium drops them once styled).

## Follow-ups
- LOW (reviewer): in `scrollbar.css` the forced-colors rule probably needs `forced-color-adjust: none` on the thumb (as `popup.css` does for the recording dot) to take effect; not verified in Windows high-contrast mode.
- Optional: thumb grows to 8px on hover (Windows 11 style) for a bigger target.
- Owner to confirm on screen: no empty strip on «Сегодня»/«Записи» (narrow and wide window, long list, and with a long agent thread); scrollbar in both themes.

## Changed existing tests
None changed in behaviour. `windowscroll363` reads `scrollbar.css` since the block moved out of `base.css`.

## Review
Reviewer (opus): changes requested (HIGH: same leak in the agent panel via `.ag-vh`) -> fixed; re-review: approve (one low, above). ux-expert (opus): «доработать» (thumb contrast, drag state, popup scrollbar) -> fixed, not re-run. security-auditor: not run (CSS/tests only, no trust boundary; owner named reviewer + ux-expert).
