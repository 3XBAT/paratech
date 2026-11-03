# TASK-367 — спокойные статусы: значок + цветной текст, один акцент

Combined round with TASK-365 (one branch, one PR).

## Done
- `StatusChip` / `.chip` / `.s-*`: no padding box, fill, border, double rim or dashed variant; only icon (or the red recording dot) + coloured text. Colours: recording and fail `--rec-t`, done `--ok-t`, waiting/attention `--wait-t`; net and neutral `--muted`. All screens that use the shared chip change at once (Today, Records, Subjects, Assignments, Schedule, Journal, Settings, tickets, popup, agent panel).
- In-progress (`.s-run`: «Пишется конспект», «Расшифровка · 42 %», «Отправляется», «Скачивается») is muted: its hue was the same as the app accent. Urgent deadlines on Today moved from "run" to "wait" (amber text + clock) to stay noticeable; `.ag-asg-late` uses `--rec-t`.
- «Остановить» (Records and popup): a normal button (`btn stop`), only the square icon is red. `.btn.danger` lost its red ring (red text stays).
- Banners (`.note`, `Banner`, `SourceAdvice`, popup): no tint and no coloured border; surface `--raised` with a neutral `--edge` contour (accent text on it is 5.45:1 light / 6.76:1 dark, the progress track stays visible); only the icon is coloured. Fail has its own `error` glyph, wait keeps `warn`, so they differ without colour.
- Source picker marks: coloured text instead of boxes. Today `.today-lesson.live`: red tint removed. Agent `.ag-confirm` / `.ag-ask` frames removed (heading word stays coloured; ask caption is `--text` 600 + `help` icon). `.today-problem` button: underline + focus ring on hover/focus since the chip lost its box.
- Tokens `--rec-b`, `--ok-b`, `--net-t`, `--net-b` removed; the rail and screen layouts are untouched.
- Tests: `status367` (CSS scan: no semantic background/border/ring on status classes, dead tokens absent, StatusChip markup = icon/dot + word, every status has a label and icon, stop button, banner icons), `contrast.test.mjs` (status text >= 4.5:1 on six surfaces in both themes, dot 3:1, banner text/icons, accent on `--raised`).

## Deliberately kept
- `<mark>` highlights (search hits, current paragraph) keep `--wait-b`/`--run-b`; neutral surfaces (`.ag-chip`, `.ag-pill`, count pill, source-picker letter); meters (`.dot`, sound level bar, `.mload` bar); text-only colours (`.problem`, `.warning`, `.slow`, `.tile.bad`, `.drop-report` icons). Chips that had no icon («Просрочено», «завтра», «Не установлена») stay word-only.

## Decisions to confirm
- Banners keep a neutral 1px edge contour (not coloured) so they still read as boxes; say if fully borderless is wanted.
- `.btn.danger` also lost its red ring (slightly beyond «Остановить»; same rule).
- In-progress statuses are muted grey, not orange; amber is left for «needs attention» and urgent deadlines.

## Follow-ups
- Other fail chips still use the `warn` glyph (only banners/result lines use `error`).
- Owner to check on screen both themes: «Идёт запись · 0:03:48» (red dot + red text), «Конспект готов» (green check + green text), «Остановить» looks like «Открыть» with a red square; banners are noticeable enough; unframed confirm/ask cards in the agent panel are distinct; forced-colors mode.

## Changed existing tests
- `recordingrow`: stop button class `btn stop` instead of `btn danger`.
- `today`: urgent chips are `s-wait`, not `s-run`.
- `contrast`: token list without the removed tokens; chip-on-tint loop reduced; new banner and status checks.

## Review
Reviewer (opus): changes requested (banner surface failed AA for the ghost button and hid the progress track) -> fixed; re-review: approve. ux-expert (opus): «доработать» (see TASK-365) -> fixed. security-auditor: no findings (CSS/markup only).
