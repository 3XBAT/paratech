# TASK-196 — check steps

No visible change in the app yet: this task only prepares the HTML for the PDF («Скачать PDF» arrives with TASK-197/198).

1. Read docs/adr/0020-document-pdf.md: status Accepted, decision option (b) (WebView2 print-to-PDF), open questions at the end (.pptx, sources/footer).
2. From paratech/ run `go test -race ./desktop/internal/adapters/pdf/...`: passes.
