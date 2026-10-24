# TASK-139: agent panel waiting indicator

Status: done. Frontend only.

## What was done
- «Отправка…» and the job-less «Работа идёт» card are replaced by one waiting element: three dots (`--accent-fill`,
  `ag-breath`, staggered), `role="status"`, hidden text «Запрос выполняется», static dots under `prefers-reduced-motion`.
- `EntryModel.waiting` (call running, no refusal, no update yet); new `CardModel` type `waiting` for PLANNING/EXECUTING
  without a core progress card, keeps «Стоп» while EXECUTING. The core `progress` card with a job is unchanged.
- `src/agent/Waiting.tsx` (new), `AgentCards.tsx`, `agent.css`, `agentView.ts`; tests in `test/agent.test.mjs`.

## Decisions to confirm
- Only the frontend part over today's state. The `agentturn.Progress` port of ADR-0026 (request state published before the
  Hub call) is Go work of TASK-140/143; until then the entry-level dots show while `update === null`.
- `.ag-vh` visually-hidden class added to agent.css (none existed).

## Follow-ups (reviewer lows, not fixed)
- The live region is re-mounted at the transition entry-level dots -> waiting card (and if the card index changes):
  some screen readers may announce it twice or not at all. Fix: one always-mounted `role="status"` per request.
- Edge: dots next to a stopped/problem card if an EXECUTING update carries a stop reason (unconfirmed in the core).
- Stagger 0.2s/0.4s of a 2.4s cycle is subtle; consider ~0.4s/0.8s.
- `.ag-wdot` duplicates `.ag-dot`; CSS test is a formatting-sensitive regex.

## Changed existing tests
- `agent.test.mjs` progress-card test: the job-less PLANNING card is now type `waiting` (was `progress`).
- `work.test.mjs`: fixture EntryModel gets `waiting: false`.

## Security
Auditor: no findings (fixed text only, no HTML sinks, no dependencies).
