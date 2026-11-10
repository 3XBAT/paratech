# TASK-403 — «Билеты к экзамену» показывают все части файла билетов

Owner answer 6 of 2026-11-08; the UI half of TASK-402 (binding `TicketSet.files`). Frontend only, by the frontend agent.

## Done
- A done set with several parts lists them in the card: «Билеты 1–50», «Билеты 51–100» (en dash; `Билет N` for a one-ticket part; `Часть N` by position when the range is unknown, 0/0). The file name is never shown.
- Every part has its own small open icon (tooltip and accessible name «Открыть «Билеты 1–50»»), its own busy spinner and its own result line (short «Открыта копия.» on success; the binding's fixed text on failure, the part stays retryable). Results and busy keys are per `materialId`.
- Order of the done block for several parts: split note «Файл разделён на части: не помещается в один документ.» (only when more than one part), plural file note «Файлы лежат в папке …», the parts list, then the muted open/generated notes. One part (or a reply without `files`): the card is exactly as before, «Открыть .docx» word button included.
- One undo icon moves all parts; tooltip «Вернуть все N файлов во «Входящие»» (plural). While one open runs, the other icons say «Открывается другой файл.».
- After undo/confirm/cancel of a set, its material ids are dropped from the open results (no stale lines).
- Pure helpers in `ticketsView.ts` (`filesOf`, `partLabel`, `partsList`, `partsNote`, `undoLabel`, `fileNote(subject, parts)`, `materialIdsOf`, `withoutResults`); `FilesList` in `TicketsParts.tsx`; css `.tk-files`.

## Decisions to confirm
- The split-note wording is the one fixed by the task; ux-expert found it reads like a defect («не помещается в один документ»: Word holds any length) and suggested one line: «Билетов много, поэтому ответы разбиты на N файлов по порядку билетов. …». Not applied, since the owner's acceptance text names the string. Owner may want to reword it.
- A one-part set stays with the word button «Открыть .docx» while a multi-part set uses icons (one action, two forms): required by «одна часть — как сейчас». ux-expert suggests one form later (row with an open icon in both cases).

## Follow-ups
- Partial undo (TASK-402 decision): when the student moved some parts by hand, only the remaining ones go back and the UI does not say so. Needs a core field (count of parts left) + binding + line «Часть файлов уже перемещена…».
- A failed open (file moved/deleted) names the cause but has no way-out text (ux low); the same as in the single card before.
- The orphaned-parts follow-up of TASK-402 is unchanged.

## Changed existing tests
`test/tickets.test.mjs` only: `openBusy: true` -> `openingId`, `openResult` -> `openResults` per material (SetCard props changed); the source assertion `lib.openMaterial(set.materialId)` -> `lib.openMaterial(materialId)`; «three parts» counts the undo button by aria-label; the per-part state test rewritten for `aria-disabled` + reason. `iconbuttons368b.test.mjs` unchanged and green.

## Verification
`npm run test` (2394 pass) and `npm run build` in `desktop/frontend`.

## Review
Reviewer: approve (one medium = partial-undo follow-up above, lows: disabled reason, stale results, brittle tests, QA recipe — fixed). ux-expert: «доработать» with two must-fix copy items (repeated success note: fixed; split note wording: kept per acceptance, see decisions) plus order of lines, undo tooltip (fixed). Security-auditor: no findings. No re-review after the fix commit (copy, order and test changes only).
