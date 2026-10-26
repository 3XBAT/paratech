// Package pdf renders a student paper (values.Document) as PDF (ADR-0020, option (b)): printHTML builds a
// self-contained print page from the document, laid out as adapters/docx lays out the same document, and a hidden
// WebView2 of our own prints it with PrintToPdf (Windows only; other OSes get a stub).
//
// Every text is data: it is escaped, and the page has a Content-Security-Policy without network, no scripts and no
// external resources, so neither the student's nor the model's text can add markup, run code or fetch anything.
package pdf
