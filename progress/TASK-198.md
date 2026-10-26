# TASK-198 — ExportPDF: PDF next to the .docx in «ДЗ»

## Done
- Domain (`work`): `File.Year` (optional JSON `year`), `Writing.PDF *File` (`pdf`, omitempty), `AttachPDF` (done only, not a presentation, only for `ContentKey()`), `PDF()`, `PaperYear`, `Files()` includes the PDF, `Finish` drops it. `homeworkdoc.PaperPDFName`.
- Use case `cases/turnkey`: `PaperPDF` port, `ExportPDF` (sync, 60 s timeout, per-work lock, fast path if the PDF exists), shared `paperDocument` for .docx and PDF. `ErrPDFUnsupported`, `ErrPDFFailed`.
- `adapters/pdf/pdf.go`: `Renderer` (printHTML + Printer.Print), error mapping.
- `input/wails`: `TurnkeyService.ExportPDF`, `pdfMaterialId`, two fixed texts; log has work id and a code only.
- `app`: one `pdf.NewPrinter(<data dir>/webview-pdf)` (separate from the Wails WebView2 profile); stub off Windows.
- ADR-0025 addendum.

## Decisions to confirm
- Title-page year: stored in `work.File.Year` at assembly (no migration). A paper stored before the task (year 0) falls back to the year the work last changed (clock zone); `AttachPDF` writes it into the paper so later PDFs agree.
- Profile is read at the click (a renamed student shows the new name in the PDF).
- "New generation resets the PDF": `Work.PDF()` returns nothing at once, the stale `Writing.PDF` stays until `fileWriting`/`Finish` so the old PDF goes to «Входящие» with the old paper.
- Errors: caller cancel stays `Canceled` (ErrUnavailable); print timeout and other errors -> ErrPDFFailed; `errors.ErrUnsupported` -> ErrPDFUnsupported.
- Outside the task file list: `input/agent/turnkey.go` (`writingFiles` lists the PDF so Undo of write_work takes it to «Входящие»; also a removed/missing file counts as already taken back, as in `unfile`). The rule applies to every file of the writing; a student can Restore a removed file later and it lands in «ДЗ».
- Review fixes: per-work lock plus "PDF kept meanwhile is the answer" (real WebView2 PDFs differ by bytes, so a double click would otherwise give ErrChanged and a stray PDF).

## Follow-ups
- Crash between FileHomework and works.Save leaves an unreferenced PDF in «ДЗ» (could unfile on next export).
- Drops use the caller's ctx; a cancelled ctx can leave a copy in «ДЗ» (WARN logged). Same-content PDF whose material was removed gives ErrChanged once (retry works).
- TestExportPDFDoubleClick busy-wait is unbounded.
- Real «Скачать PDF» check by the owner: docs/qa/tasks/TASK-198.md.

## Changed existing tests
`input/wails/turnkey_test.go`: `ExportPDF` added to `fakeTurnkey`; `"pdfMaterialId":""` added to two exact JSON expectations. `desktop/tests/turnkey_test.go`: `newTurnkeyEnv` wires the PDF port (no assertions changed).

## Security
Auditor: no high/medium; low: double click without a saved PDF (fixed by the lock). Reviewer: approve with fixes (2 medium fixed, re-review approve).
