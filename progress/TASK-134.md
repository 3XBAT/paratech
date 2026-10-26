# TASK-134: tickets screen (UI)

Status: done (PR #197 plus the finish PR from branch ralph/20261026-task-134b). Frontend only; no Go, Hub or dependency change.

## Done (#197)
- Screen «Билеты» (from «Предметы» -> «Билеты»): subject select, paste form (Propose), material select (ProposeFromMaterial), the subject's sets (List).
- Set card: status chip, progress «Готово N из M», per-ticket coverage chip, report line, stalled state with «Запустить снова», wait chips from the real job statuses.
- Paid run: estimate and cap in USD; Confirm only from the card button; over cap needs a ticked checkbox (the core enforces too). Cancel and Undo. Polling every 3 s while running and not stalled.
- Titles are plain React text. Binding wrapper `src/bindings/tickets.ts` with strict parsers and fixed Russian error texts.
- Tests `test/tickets.test.mjs`.

## Done (finish, 134b)
- «Открыть .docx» on a done set with `materialId`, through `libraryApi.openMaterial(id)` (TASK-177); ids only. The note and the success line say a COPY is opened and Word edits do not reach the library. Failure shows the binding's fixed text. Guarded by the shared run()/busy logic.
- Pure helpers `canOpenDocx`, `openResultView` in `ticketsView.ts`, tested. Removed unused export `capTickets`.
- npm test 644 pass, build ok.

## Decisions to confirm
- Entry point under «Предметы» (no new rail item).
- Wait-chip wording («Ждёт зарядку» differs from «ждёт питания от сети» in recordings).
- The note appears only when the open button is shown.

## Follow-ups
- Move «Билеты» button label into `libraryView.ts` texts.
- Component behaviour (layout, css, polling, open button) is checked only by SSR tests; owner check needed on screen.
- Undo button stays after a successful undo until the next reload.
- Reviewer lows: the copy sentence is shown twice after a successful open; `onOpenDocx` error callback is nearly dead and a stale result line stays while the next open runs; the page wiring test reads the source with a regex (brittle).
- Re-importing an edited copy as a new version is a Go follow-up (TASK-177).

## Changed existing tests
`test/tickets.test.mjs` "titles are rendered as plain text": it asserted the open button is absent; now asserts it is present (the feature intentionally changes that).

## Security
security-auditor: no findings. Reviewer (finish): approve, 4 lows (above). Earlier pass (#197): 1 high and 2 mediums fixed, re-review approve.
