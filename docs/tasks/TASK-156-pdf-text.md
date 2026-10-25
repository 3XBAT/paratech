# TASK-156: Text of PDF files (pure Go)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-150
- **PRD sections / AC:** ADR-0027 «Text», Open 5; ADR-0024 «Text extraction» (amended); hard rules 1, 9
- **Branch:** ralph/<date>-task-156

## Goal
A typed PDF (exported from Word, Notion, a lecturer's slides) gives text with page locators; a PDF without a text
layer is reported as a scan.

## Allowed to change
- `paratech/desktop/internal/adapters/doctext/**`
- `paratech/desktop/internal/domain/library/material/{material.go,text.go}` (+ tests): PDF text statuses
- `paratech/desktop/go.mod`, `go.sum`; `docs/adr/0027-own-notes.md` (addendum naming the dependency)
- `docs/refs/<pdf-lib>.md` (facts used, from the library source)

## Out of scope
- OCR of scanned PDFs (TASK-167).

## Acceptance criteria
- [ ] One pure-Go dependency (no CGO), licence MIT/BSD/Apache, chosen after reading its source; recorded in the ADR
      addendum with version and why. If none extracts Cyrillic with ToUnicode maps reliably, stop and report.
- [ ] Page locators; limits as ADR-0024 (text 4 MiB, 5000 fragments) plus a page cap and a time budget per file;
      a panic in the library is recovered and the file is `failed`.
- [ ] A PDF with no text on any page gets status «скан, текст не извлечён».
- [ ] Fixtures: Cyrillic text PDF, scan-only PDF, encrypted PDF, malformed PDF (fuzz, no panic).
- [ ] `go vet`, `go test -race` of the touched packages, the no-CGO check and the architecture test pass.
