# ADR-0020: PDF of a student paper

- **Status:** Proposed (the owner decides)
- **Date:** 2026-10-20
- **Source:** TASK-104 (PDF without new dependencies), TASK-105; hard rules 3 and 9.

## Context
TASK-104 asks for the same document as PDF, preferably «HTML view + WebView2 print to PDF», if Wails v3 allows it.
Checked in `wails/v3@v3.0.0-beta.29`:
- `ICoreWebView2_7.PrintToPdf` exists only in `internal/webview2/pkg/webview2`; Go forbids importing `internal` from
  our module.
- The public API has only `WebviewWindow.Print()`, which runs `window.print()` (`webview_window_windows.go:302`):
  the print dialog, no file path, no silent mode. `NativeWindow()` gives the HWND, not the WebView2 controller.

So a silent PDF is not reachable through Wails.

## Options
- **(a) HTML view + `Print()`.** Render the `values.Document` as print HTML (CSS `@page` margins, GOST fonts) in a
  window and call `Print()`; the student picks «Microsoft Print to PDF» and a file name. No dependency, little code;
  needs a dialog, and the TOC has no page numbers (CSS paged media is weak in Chromium).
- **(b) Own WebView2 COM binding.** A hidden window with our own WebView2 environment and `PrintToPdf` called
  through raw vtables via `syscall`. No dependency, silent, but a few hundred lines of unsafe COM code that breaks
  with WebView2 changes; hard to test.
- **(c) Pure-Go PDF writer.** Our own layout and PDF writer with an embedded or system TTF; Cyrillic needs a CID
  font with subsetting, plus line breaking, justification and page numbers. Silent and testable, but large (weeks);
  a library (e.g. a gofpdf fork) would need an ADR for the dependency.
- **(d) Word COM automation.** Open our .docx in Word and save as PDF: the best fidelity (same file, real TOC page
  numbers), but only with Word installed, and COM automation of Office is slow and fragile.

## Recommendation
(a) for MVP: «Сохранить в PDF» opens the print view of the same document and the system print dialog; the .docx stays
the primary file. Offer (d) as a silent path later when Word is found. Not implemented in TASK-104.

## Consequences
- Until the owner decides there is no PDF; TASK-105 plans with .docx only.
- Whichever option is chosen renders from `values.Document` (format-neutral), never from model text directly.
