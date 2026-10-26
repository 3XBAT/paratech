# TASK-199 — «Скачать PDF» on a ready paper or lab report

## Done
- bindings/turnkey.ts: `exportPdf(id)` over `TurnkeyService.ExportPDF`; `pdfMaterialId` parsed strictly (missing = "", wrong type throws); the two fixed core texts of TASK-198 added to `errorTexts`.
- workView.ts: texts, `pdfOffered` (ready paper or lab report, not a presentation), `pdfTarget`, `exportPdf` (no call when not offered or a PDF exists), `openPdf` (openMaterial by id, a copy).
- WorkCard.tsx: «Скачать PDF» while `pdfMaterialId` is empty («Сохранение PDF…» and disabled during the call); with an id: the line «PDF сохранён в библиотеке рядом с файлом Word.» and «Открыть PDF»; the copy caption also covers it. Both go through `run` (singleFlight + the card error line). No button while writing or for presentations. A new generation has an empty id, so «Скачать PDF» returns.
- test/work.test.mjs: 9 new tests.

## Decisions to confirm
- The saved line shows whenever `pdfMaterialId` is set (also on a re-opened card), not only right after the click.

## Follow-ups (reviewer lows)
- The busy label is checked by a source regex; a pure `pdfButtonLabel(busy)` helper would test it by behaviour.
- «Открыть PDF» needs the `library` prop (always passed in production).

## Changed existing tests
None.

## Security
Auditor: no findings. Reviewer: approve (one formatting nit fixed).
