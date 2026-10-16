# ADR-0012: Visual direction, font and icons of the desktop UI

- **Status:** Accepted
- **Source:** TASK-064; design brief and concept in `docs/design/`; package notes in `docs/refs/ui-fonts-icons.md`

## Context
The UI had no design system. The owner chose the concept «Эфир», calm version (`docs/design/concept-final.html`,
`docs/design/brief.md`): side rail, schedule screen, restrained palette, status chips with colour + icon + word.

## Decision
- Direction: «Эфир», calm version, as in `docs/design/`. Tokens live in one file, `src/ui/tokens.css`.
- Font: `@fontsource-variable/golos-text` **5.3.0** (OFL-1.1), bundled with the app; only the `cyrillic` and `latin`
  subsets, own `@font-face` in `src/ui/fonts.css`. No network fonts. The family is set in one CSS variable (`--font`).
  Inter and Onest (both OFL-1.1) are recorded alternatives, not dependencies: switching means a new package and that variable.
- Numbers use `tabular-nums` of the same font; no monospace UI font.
- Icons: one inline SVG sprite (`src/ui/Icon.tsx`) with the concept's own icons. A missing icon is copied as source from
  Lucide (ISC) and listed in the refs file; no icon npm package.
- No UI library and no CSS framework: plain CSS and own small components in `src/ui/`.
- Theme (system / light / dark) is stored in the webview's `localStorage`: TASK-064 does not change bindings.
  A core setting shared with the popup is a possible follow-up.

## Consequences
- One new runtime dependency (the font files, ~60 KB of woff2 in the build).
- Every colour pair is checked by `test/contrast.test.mjs` against `tokens.css` (WCAG 2.x).
- The theme choice lives in the webview profile, not in the core; clearing the WebView2 data resets it to «Как в системе».
