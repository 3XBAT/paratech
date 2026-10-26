# TASK-175 — beta UI fixes (Today with panel, composer, Ctrl+K, accent, wait card)

## Done (frontend only)
- Today: `.today` is a size container; two columns only at >= 62rem (left `minmax(30rem, 1fr)`, right up to 22rem), lesson row status column at >= 38rem; "Спросить paratech" hint spacing and wrapping chips (CSS only).
- Composer: `<textarea>` autosizing to 6 rows, Enter sends, Shift+Enter newline, no send during IME (`isComposing` / keyCode 229); maxLength 4000, disabled when busy. The field moves to the bottom after the first entry via CSS `order` (same DOM node, focus and draft kept).
- Ctrl+K by `event.code === "KeyK"`; a second Ctrl+K on an open panel closes it and blurs focus inside the panel (`hide()` added to the resizable panel hook; TASK-143 resize untouched).
- Accent check centered (50%/50% + translate); waiting card dots centered, padding 12/16.
- Tests: `test/agent.test.mjs` (isFocusPanelKey by code incl. Russian layout, composerKey, panelShortcut, inputPlacement, rendering). 636 pass; build green.

## Changed existing tests
`test/agent.test.mjs`: Ctrl+K fixtures now carry `code`; two `/<input.../` regexes became `/<textarea.../` (field is a textarea now).

## Decisions to confirm
- With the panel open and focus on the page, the first Ctrl+K closes the panel (spec wording «повторный»). Alternative: close only when focus is already inside the panel, otherwise focus the field.
- `.ag-card-wait .ag-actions` also centers the «Стоп» button (small addition).

## Follow-ups (reviewer lows, not fixed)
- Ignore `e.repeat` for Ctrl+K (holding flickers the panel).
- Recompute textarea height on panel width change (only on draft change now).
- Tab order differs from visual order when the field is at the bottom (price of `order`).
- Use `--sp-12` / `--sp-4` tokens instead of px in today.css.
- After closing, focus lands on body, not the last page element.
- CSS not checked visually; check-mark -60% translate is an optical guess.

## Security
security-auditor: no findings. Reviewer: approve, no blocker/high.
