# TASK-428: scrollbars appear only while scrolling (as in Claude); gap under «Ссылка на пару»

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** none (parallel with TASK-429 and the sprint tasks: no shared files)
- **PRD sections / AC:** §4.3 (Wails/WebView2 UI); owner 2026-11-10; follows TASK-363 (colors, contrast, forced-colors)
- **Агент:** только UI: presentation, no student data (ADR-0033 §1)
- **Branch:** ralph/<date>-task-428

## Goal
Scrollbars of the main window, the agent panel and the popup are invisible at rest, show while the student scrolls
(wheel, thumb drag, keyboard) or points at the scroll edge, and fade out about 1 s after; nothing in the layout moves
when they show. Plus the missing gap between the «Ссылка на пару» block and the tabs «Пары / Файлы / Задания».

## What WebView2 (Chromium) allows — and what not to use
- No CSS-standard overlay scrollbar. `overflow: overlay` is gone (an alias of `auto` since Chromium 114): do not use.
  No WebView2/Chromium feature flags (`AdditionalBrowserArguments`) either.
- `scrollbar-width` / `scrollbar-color` stay forbidden (Chromium then ignores `::-webkit-scrollbar`, TASK-363 guard).
- Allowed and enough: `::-webkit-scrollbar*` per element (`.x::-webkit-scrollbar-thumb`), a class toggled from JS,
  the standard `scroll` event caught on `document` in the capture phase (it does not bubble), `pointermove`,
  `scrollbar-gutter: stable` (standard, does not disable `::-webkit-scrollbar`).
- `transition` does not apply to scrollbar pseudo-elements. The fade is a **spike**: try on this machine (a)
  a registered custom property (`@property`) transitioned on the scroller and used in the thumb color, (b) the
  `border-color: inherit` trick (thumb takes a transitioned color from its scroller). Keep the first that repaints
  smoothly in WebView2; if neither does, hide at once after 1 s (no fade) and record it in progress. Do not ship an
  untested technique.

## Design
- `src/ui/scrollActivity.ts` (new): `installScrollActivity(doc: Document): () => void`.
  - `scroll` (capture, passive) → the scrolled element (or `doc.scrollingElement`) gets class `scroll-active`; a timer
    per element (WeakMap) removes it 1000 ms after the last scroll.
  - `pointermove` (passive, at most once per animation frame): when the pointer is over the scrollbar strip of the
    nearest scrolling ancestor of the target (strip width = `offsetWidth - clientWidth - borders`, vertical and
    horizontal), the class goes on and the timer restarts; leaving the strip lets the timer run out.
  - A thumb drag (pointerdown in the strip until pointerup / pointercancel) keeps the class on.
  - Keyboard scrolling (arrows, PgUp/PgDn, Space, Home/End) produces `scroll` events: no key handling needed.
  - Installed once in `src/main.tsx` and `src/popup/main.tsx` (the agent panel is in the main window document).
- `src/ui/scrollbar.css`: thumb `transparent` at rest; `.scroll-active::-webkit-scrollbar-thumb` uses `--scroll`,
  `:hover` / `:active` `--scroll-hover` (tokens and sizes of TASK-363 unchanged); `prefers-reduced-motion: reduce` → no
  fade; `forced-colors: active` → the thumb is always visible (`CanvasText`, `forced-color-adjust: none`, the TASK-363
  follow-up), no hiding.
- No layout shift: every scroll container (`overflow`/`overflow-y: auto|scroll` in `styles.css`, `agent/agent.css`,
  `popup/popup.css`, `sourcePicker/sourcePicker.css`, and inline styles in `src/**/*.tsx` if any) gets
  `scrollbar-gutter: stable`; the track keeps its 10 px, only the thumb color changes. Exemptions only where a gutter
  would look wrong (a horizontal-only `pre` like `.template`, the agent input textarea), listed in the test with a reason.
- «Ссылка на пару»: in `styles.css` the block keeps a 24 px gap (the `.section + .section` rhythm) to the tabs, in
  every state (link shown, form open, refusal line, result line), wide and narrow window (container ≤ 900 px).

## Allowed to change
- `paratech/desktop/frontend/src/ui/scrollbar.css`, `src/ui/scrollActivity.ts` (new)
- `src/main.tsx`, `src/popup/main.tsx` (one install call each)
- `src/styles.css`, `src/agent/agent.css`, `src/popup/popup.css`, `src/sourcePicker/sourcePicker.css`
  (only `scrollbar-gutter` lines and the call link gap)
- `paratech/desktop/frontend/test/*` (new tests; `windowscroll363.test.mjs` may be extended, not weakened)

## Out of scope
- Colors, thumb sizes, tokens (TASK-363 values stay; `contrast.test.mjs` unchanged).
- Any change to `AgentPanelView.tsx`, `agentView.ts`, `agentState.ts`, `recordingRowView.ts` (TASK-418/426).

## Acceptance criteria
- [ ] At rest no thumb is visible anywhere (main window sheet, tree, dialogs, agent thread and plan, chat menu, popup,
      source picker); wheel, thumb drag, keyboard scrolling and pointing at the scroll edge show it; it goes about 1 s
      after the last activity (fade or instant per the spike, recorded in progress).
- [ ] No layout shift when a thumb shows or content starts to overflow (screenshots before/after in progress).
- [ ] Forced colors: thumb always visible in `CanvasText`; reduced motion: no fade.
- [ ] Gap between «Ссылка на пару» and the tabs in every state, wide and narrow.
- [ ] Tests: `scrollActivity` with fake timers and stubbed geometry (scroll → class on, off after 1000 ms, restarted by
      a second scroll; pointer in the strip → on; outside → off after the timer; drag keeps it; uninstall removes the
      listeners); CSS guards: thumb transparent at rest, visible only under `.scroll-active`, forced-colors block
      unconditional, no `scrollbar-width/-color`, every scroll container has `scrollbar-gutter: stable` or a listed
      exemption, the call link gap rule exists; both entries call `installScrollActivity`.
- [ ] UX check (coordinator-round.md 3a): discoverable (the edge hover shows the bar before a drag), no dead ends,
      states (rest, active, hover, drag, forced colors), quiet UI (one accent, no new colors).
- [ ] `npm --prefix paratech/desktop/frontend run test` and `run build` pass.

## Notes
- jsdom has no layout: geometry is stubbed with `Object.defineProperty` on the elements in the test.
- Manual check on this machine (Windows 10, WebView2): main window, agent panel with a long thread, popup; light and
  dark theme; high contrast on.
