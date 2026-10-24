# TASK-134: tickets screen (UI)

Status: pending in tasks.json (one criterion unmet: «открыть .docx»). Frontend only; no Go, Hub or dependency change.

## Done
- Screen «Билеты» (reached from «Предметы» -> «Билеты»): subject select, paste form (Propose), material select (ProposeFromMaterial), the subject's sets (List).
- Set card: status chip, progress «Готово N из M», per-ticket coverage chip (covered / partial / не покрыто / ответ не получен / ждёт ответа), report line, stalled state with «Запустить снова», wait chips (network, quota, power, retry) from the real job statuses.
- Paid run: estimate and cap in USD; Confirm is called from one place (the card button, explicit click). Over cap needs a ticked checkbox (`budget.overCap` only; the core enforces too). Stalled restart shows no checkbox. Cancel and Undo. Polling every 3 s while a set is running and not stalled.
- Titles are plain React text (tested: no HTML, links, markdown). Binding wrapper `src/bindings/tickets.ts` with strict parsers and fixed Russian error texts.
- Additive edits to shared files: App.tsx, nav.ts, agentView.ts, main.tsx, SubjectsPage.tsx, tsconfig.test.json; new tickets.css.
- Tests: `test/tickets.test.mjs` (16+). npm test 582 pass, build ok.

## Unmet
- «Открыть .docx»: no binding opens a library file (no path crosses the binding). The card only says the file lies in the «Билеты» folder of the subject. Needs a core task: open/reveal a library material as a desktop action.

## Decisions to confirm
- Entry point under «Предметы» (no new rail item).
- Wait-chip wording («Ждёт зарядку» differs from «ждёт питания от сети» in recordings).

## Follow-ups
- Core binding to open a material file (above).
- Remove unused export `capTickets`; move «Билеты» button label into `libraryView.ts` texts.
- Component behaviour (layout, css, polling) is checked only by SSR tests; owner check needed on screen.
- Review lows: Undo button stays after a successful undo until the next reload.

## Changed existing tests
None.

## Security
security-auditor: no findings. Reviewer: first pass 1 high (wrong WAITING_* job status strings) and 2 mediums, fixed; re-review approve.
