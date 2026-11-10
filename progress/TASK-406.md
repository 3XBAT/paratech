# TASK-406 — popup: «Открыть ещё раз», «Подключиться» during the recording, neutral dot (frontend)

Owner 2026-11-08 (answers 11, 12, 13). Go binding: TASK-405. a11y follow-ups: TASK-370.

## Done
- Opened link: the button reads «Открыть ещё раз», stays active, opens again by the same subject ID; double click opens once («Открытие…», busy); a failed repeat keeps the label and shows the amber line. The note «Ссылка открыта. Запись пока не идёт.» only before the recording.
- During the recording started from the popup: a full-width secondary word button «Подключиться» / «Открыть ещё раз» under «Остановить»/«Скрыть». `StartedRecording` (pure, joinView.ts) ties the started lesson prompt to the first recording the popup sees running and keeps asking `callSubject` with that prompt id; dropped when that recording ends or another one appears. A recording not started from the popup gets no button (the core answers ""). Opened state is kept per prompt id across the start and across popup closes; «Ссылка открыта.» is announced in the running view.
- Dot: still muted dot and muted heading in the offer; red pulsing dot only in the recording view and the silent-source advice (`dotClass`/`topClass`, view.ts).
- a11y: persistent `role="status"` regions for the quiet join line and the failure line (mounted before their text, empty height 0 or one reserved line).
- Offer layout (review rounds): while the join button is visible, «Начать запись», join and «Не записывать» are each a full row in every phase, so no row moves under the pointer between ready/opening/opened (a high from the re-review). The join line is reserved (one line) so the texts do not jump. The window grows to its natural height (existing HeightFitter, now also with the join row; Go max 440, anchored bottom-right) so the buttons are not cut at larger text sizes.

- Final round: the reserved join line is no longer counted as a message (a browser warning + the join button keeps the advice in full); «gone» keeps the join row as a visible disabled button (last label or «Ссылки нет», `reopen` field in JoinState, dropped on refresh/new prompt/clearProblem) with the note and hover «Ссылки на пару уже нет. Её можно добавить на странице предмета.» so no row moves under the pointer.

## Decisions to confirm
- Wording of the popup «gone» text now names the place («…на странице предмета»); TASK-370 said «without offering to add one». The main window text (callLinkView.ts) is unchanged.
- Layout reversal of TASK-370: «Подключиться» no longer shares a row with «Не записывать»; three full rows. Reason: the label «Открыть ещё раз» (~134 px) spills out of a half row at 125% text, and a row that changes between phases puts another button under a double click.
- The real popup is 360x300 (spec.go), not 300 wide; the criterion «300 px» is read as «does not overflow».
- «Открыть ещё раз» kept as the label in the running view (owner's wording); ux-expert suggested «Открыть пару ещё раз» for context.

## Follow-ups
- Text-size scaling: the growth logic (and the old error growth) assumes CSS px = DIP; if WebView2 scales content by the Windows text size, the window under-asks. Needs a scale factor (innerHeight vs window DIP height). Not checked on screen.
- «gone» note (~63 chars) may wrap to 2 lines in the one-line reserved join line; the texts grow by a line (window grows upward, buttons stay). Same family as the failure-line item below.
- `clearProblem` (a start/skip attempt) also drops the gone row, so rows can shift if the start then fails fast; fix idea: clear only «failed» there.
- The join row appears after the first paint (the `callSubject` answer), so «Начать запись» moves up one row at that moment (ux-expert/reviewer low). Fix idea: reserve the row for a prompt known to have a subject.
- The empty reserved line is a visible gap; one live slot last in `.texts` for confirm/note/failure would be calmer (ux-expert nice-to-have). The confirmation may wrap to 2 lines at 150%.
- Nits: `joinTexts.noLink` is unreachable from the controller; missing tests for «load finds no link keeps gone» and the gone row in the running view; at 100% the window may grow with a browser warning.
- The confirmation «Ссылка открыта…» comes back on a later re-show of the same prompt (opened state survives close); show it only in the showing where it was opened.
- A failed open adds the failure line below the reserved empty line, so the window may grow by one line.
- Advice protective part is cut when collapsed (shared text in sourcePicker/view.ts): put «Не выключайте звук…» first.
- Join button in the running view for a recording started from the main window/tray: not shown (criterion says «начатой из этого окна»).
- Lows from review: a recording never seen by the popup can be bound to the prompt (core answers "" so no wrong button).

## Changed existing tests
`popupJoin370.test.mjs`: view shape (label, live), opened button clickable, closing keeps the opened state, late open after close lands, confirmation not disabled, buttons/CSS (no `.joined`), banner anchors -> persistent regions, running confirmation «Ссылка открыта.», `joinOnOwnRow` true in all visible phases, layout identical across phases, collapse back to the original rule (each marked «Changed in the TASK-406 review»). `iconbuttons368b.test.mjs`: word-button list `{callLinkTexts.join}` -> `{jv.label}` (twice). `popupwindow.test.mjs`: the join-row collapse test rewritten for the original rule + new `windowGrows` tests.

Final round also changed: popupJoin370 (no_link/not_found now visible+disabled with the new note; second-open; vanished-link; layout test incl. gone), popupwindow («original rule» test now `collapsed: false` for one browser warning; the no-op extra-argument assertion removed). New tests: gone label/title, gone survives closed(), dropped by refresh/new prompt/clearProblem, layout identical in ready/opening/opened/gone.

## Review
Reviewer (opus): approve with lows; re-review after the layout high (rows moved between phases) -> fixed -> approve. Final round on the last 2 layout commits: reviewer medium (reserved line collapsed the advice) and ux-expert must-fix («gone» moved a row) fixed in 52324aaf; re-review approve (docs fixed, lows in follow-ups). ux-expert (opus): «доработать» (label spill at 125%, running-view context, growth at larger text, reserved line, gone row) -> fixed except items above. security-auditor (opus): no findings on the first and the final round.
Security: the page passes only prompt/subject ids; no link, host or URL on the page; the core re-checks that the started prompt's recording runs.
