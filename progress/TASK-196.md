# TASK-196 — ADR-0020 Accepted (option b); print-ready HTML from values.Document

## Done
- docs/adr/0020-document-pdf.md: Accepted 2026-10-26 (owner decision), option (b) WebView2 PrintToPdf; (a) and (d) not taken; open: .pptx, sources/footer.
- adapters/pdf/doc.go (package comment, no build tag) and html.go: `printHTML(values.Document) ([]byte, error)`, `ErrRender`. Same document and layout numbers as adapters/docx (constants copied: adapters may not import each other).
- Self-contained HTML5: CSP `default-src 'none'; style-src 'unsafe-inline'`, no scripts, links or external resources. Own escape function (& < > " ' plus `:` and `=`, so `http:`/`src=` cannot appear). Invalid UTF-8, control, C1 and bidi characters dropped; fonts and numbers come only from `Formatting.Normalized()`.
- Tests: papers and lab reports from homeworkdoc builders, every block kind, injection cases, no-network checks, determinism, invalid documents.

## Decisions to confirm
- docs/architecture.md unchanged (it has no adapter list).
- TOC has no page numbers (print page cannot compute them); recorded in the ADR.

## Follow-ups (for TASK-197/198)
- Page numbers use CSS `@page @bottom-center` (Chromium 131+); the ADR minimum runtime is 98: check on a real PDF with ShouldPrintHeaderAndFooter=false and record the runtime version; also that PrintToPdf margins do not override `@page`.
- Chromium `line-height:1.5` is relative to font size, Word's to the font line height: the PDF body is about 13% tighter than the .docx. Check visually; maybe `calc(1.15*1.5)`.
- Yellow placeholder background in the real PDF.
- Small test gaps: empty TOC, newline in text, `<title>` escaping.
- Security audit of the WebView2 lockdown (scripts, navigation, temp path) belongs to TASK-197.

## Changed existing tests
None.

## Security
Auditor: no findings. Reviewer: approve, lows only.
