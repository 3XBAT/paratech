# UI font and icons

Package: `@fontsource-variable/golos-text` **5.3.0**, checked in the installed package on 2026-10-16.
Source: https://fontsource.org/fonts/golos-text, repository https://github.com/fontsource/font-files (`fonts/variable/golos-text`).
Decision: ADR-0012.

## Font
- Licence: **SIL Open Font License, Version 1.1** (`OFL-1.1`), https://openfontlicense.org (the package `LICENSE` points to
  http://scripts.sil.org/OFL). Copyright 2019 The Golos Text Project Authors (https://github.com/googlefonts/golos-text).
  The full text ships in `node_modules/@fontsource-variable/golos-text/LICENSE`.
- Files used (variable `wght` axis 400–900, `woff2`), imported by `src/ui/fonts.css` with the package's own `unicode-range`:
  - `files/golos-text-cyrillic-wght-normal.woff2` (22 KB): `U+0301,U+0400-045F,U+0490-0491,U+04B0-04B1,U+2116`
  - `files/golos-text-latin-wght-normal.woff2` (38 KB): `U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD`
- Not used: `cyrillic-ext`, `latin-ext` and the package's `index.css`/`wght.css` (they would pull all four subsets).
  Characters outside the two subsets (for example `→`, U+2192) fall back to Segoe UI.
- The font has the `tnum` feature: `font-variant-numeric: tabular-nums` gives equal-width digits.

## Icons
- Own sprite in `src/ui/Icon.tsx`: the icons drawn for the concept (`docs/design/concept-final.html`), 24 grid,
  stroke 1.7, `currentColor`.
- Lucide (https://lucide.dev), licence **ISC**, https://github.com/lucide-icons/lucide/blob/main/LICENSE — the allowed source
  for missing icons, copied as SVG source, never as an npm package.
- Copied from Lucide so far: **none**. When the first icon is copied, add its name here together with the copyright line
  of the Lucide `LICENSE` file, copied verbatim.
