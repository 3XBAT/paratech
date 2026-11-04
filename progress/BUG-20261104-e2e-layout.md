# BUG-20261104-e2e-layout: defects found by the E2E layout pack (TASK-376)

No tasks.json entry. Branch ralph/20261104-bug-e2e-layout.

## Done
1. «Сегодня», «Добавить ссылку» clipped on the left: below 38rem the auto-placed `.today-ac` fell into the 4.5rem time column (row 3) with `justify-self: end`. Now `grid-column: 2 / -1`, start-aligned (and shifted by the button padding to line up with the title) in the narrow layout; unchanged in the wide container query (`pages/today.css`).
2. `.today-subject` (line clamp 2) has `title` with the full text (deadlines and «Ждут решения» rows).
3. «Предметы» tree: `title` on subject, folder and semester buttons; `.tog` has `title` = its aria-label.
4. Breadcrumbs: ancestors shrink first (`flex: 0 1000 auto`, floor `min(100%, 12ch)` only for labels longer than 12 chars, class `long`), the current crumb takes the rest (`flex: 0 1 auto; min-width: 0`); `title` on every crumb (the 24-char heuristic is gone). A first version (`flex: 0 0.001`) made the line wider than the pane on the assignment page: found by the E2E re-run, fixed. Review found a gap after the «/» from the floor on short labels: fixed (class `long`).
5. 422 on the assignment page: Wails answers 422 for every bound method that returns an error, and `TurnkeyService.WorkOfAssignment` returned `ErrTurnkeyNotFound` for the normal «no work yet» state. Now `(*TurnkeyWork, error)`: `nil, nil` (JSON null) for no work (Debug log); TS `workOfAssignment: Promise<TurnkeyWork | null>`, `loadTurnkeyBlock` null -> none. Other errors unchanged.
6. 404 at start: `quietAssets` middleware (`input/wails/assetquiet.go`, `AssetOptions.Middleware`) answers GET/HEAD of `/favicon.ico` and `/wails/custom.js` with 204 (explicit text/plain type, so the runtime's `loadOptionalScript` adds no script). `/wails/custom.js` is a Wails runtime probe that Wails itself answers 404.

## E2E layout pack, real run, before / after (this machine, 04.11, fresh exe)
| | before: обычное окно 1010x683 | after | before: 1024x640 | after |
|---|---|---|---|---|
| готово | 14 | 19 | 13 | 19 |
| сломано | 5 (Сегодня, Предметы, Задания, Билеты, Помощник) | 0 | 6 (+ Расписание: 404 at start) | 0 |
| не проверено | 3 | 3 | 3 | 3 |

Still «не проверено» (unchanged, not failures): Страница пары (no pair with data in the seed), Настройки · Разрешения (no such item in this build), Помощник · строка «Стоп» (the fake LLM answers at once).

## Decisions to confirm
- `/wails/custom.js` and `/favicon.ico` get 204 instead of 404 (the 200-with-empty-JS variant would add a request for nothing).
- Tooltips are native `title` on all tree rows and crumbs, also on text that is not cut (simple; a title only when cut needs a JS measure).
- Breadcrumb floor of 12 chars counts padding and separator (about 9 visible letters).

## Follow-ups
- ux-expert: «Добавить ссылку» is a word button in the accent colour, repeated on every pair row; per owner rules a small action should be an icon with a tooltip, accent only for «Подключиться» (TASK-364 design). Only its alignment was fixed here.
- ux-expert (out of scope): on the assignment page «Быстрый черновик» and «Новый черновик» are two stacked cards for one action.
- ux-expert: the Предметы screenshot at 1024x640 does not show the subject tree (re-shoot scrolled to the top); repeated unlabeled teacher blocks.
- Layout tests of the CSS fixes only read CSS text (no DOM in the unit tests): the real check is the E2E pack. A geometry check for crumbs (gap after the «/», `ol.scrollWidth <= nav.clientWidth`) and `.today-ac` left edge in the E2E pack would pin it.
- Very deep breadcrumb trails (N floors wider than the pane) can still overflow; paths are at most 3 levels.
- E2E watcher: a console error «Failed to load resource» has no request URL; logging method + URL of failed responses (not the body) would have found #5 at once.
- `go test -race ./desktop/internal/app/` needs `-timeout` above the 10 min default on this machine (543 s measured; CI uses its own).

## Changed existing tests
`breadcrumbs350`, `breadcrumbs356`, `library`, `libraryInbox`, `today` (exact markup gets `title`; breadcrumbs350: «short name needs no tooltip» now expects a title on every crumb); `homeworkturnkey` (case for a null reply added, none removed); `assetquiet_test` (checks the explicit text/plain 204 type; own new test); `turnkey_test` (own new test: logger records Debug).

## Review
reviewer (opus): changes requested (crumb floor gap, test quality, 204 comment, Info log) -> fixed (test quality recorded as follow-up). ux-expert: «доработать» (gap after «/», alignment of «Добавить ссылку») -> fixed; the word-button design and the stacked cards are follow-ups. Security-auditor: no findings. A re-review of the last two small commits was not run; the E2E pack was re-run on them (green at both sizes).

## Security
No findings (middleware matches two exact paths, GET/HEAD only; no new data in logs; titles are React attributes).
