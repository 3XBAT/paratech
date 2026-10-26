# ADR-0020: PDF of a student paper

- **Status:** Accepted (2026-10-26, owner decision)
- **Date:** 2026-10-20, accepted 2026-10-26
- **Source:** TASK-104 (PDF without new dependencies), TASK-105, TASK-196..198; hard rules 3 and 9.

## Context
TASK-104 asks for the same document as PDF, preferably «HTML view + WebView2 print to PDF», if Wails v3 allows it.
Checked in `wails/v3@v3.0.0-beta.29`:
- `ICoreWebView2_7.PrintToPdf` exists only in `internal/webview2/pkg/webview2/ICoreWebView2_7.go:37`, the runtime loader
  in `internal/webview2/webviewloader/env_create.go:40-117`; Go forbids importing `internal` from our module.
- The public API has only `WebviewWindow.Print()`, which runs `window.print()` (`webview_window_windows.go:302`):
  the print dialog, no file path, no silent mode. `NativeWindow()` gives the HWND; there is no public access to the
  WebView2 controller of a window.

So a silent PDF is not reachable through Wails. The owner wants an own «Скачать PDF» button with a silent PDF, not the
print dialog; the file lands next to the .docx in the library and opens like the other files.

## Options
- **(a) HTML view + `Print()`.** The print dialog, the student picks «Microsoft Print to PDF» and a file name.
- **(b) Own WebView2 COM binding.** A hidden window with our own WebView2 environment, `PrintToPdf` through raw
  vtables. No dependency, silent; a few hundred lines of unsafe COM code.
- **(c) Pure-Go PDF writer.** Own layout and font subsetting: weeks of work, or a dependency.
- **(d) Word COM automation.** Best fidelity, but only with Word installed; slow and fragile.

## Decision
Option (b).
- `adapters/pdf` builds the print page and prints it. `html.go` (all OSes) turns a `values.Document` into a
  self-contained HTML5 page: the layout numbers of `adapters/docx` (A4 `@page`, margins, font, size, line spacing,
  indent, page breaks, highlighted lab placeholders); every text escaped, bidi and control characters dropped as in
  `.docx`. The HTML is built only from `values.Document`, never from model text directly; an unknown block kind is an
  error.
- The page carries the CSP `default-src 'none'; style-src 'unsafe-inline'`: no network, no scripts, no external
  resources, no links.
- `webview_windows.go` (`//go:build windows`, stub elsewhere): its own WebView2 environment with its own user data
  folder (never Wails' windows or WebView2), a hidden window, scripts disabled, every navigation but the initial
  `NavigateToString` cancelled, then `ICoreWebView2_7::PrintToPdf` into a temporary file.
- Vtable order and IIDs come from `WebView2.h`; Wails internal is a reference only. Code carried over from Wails keeps
  its MIT notice.
- No new dependency: `golang.org/x/sys` is already in `desktop/go.mod`; no CGO.
- Scope: a finished paper and a lab report.

## Consequences
- «Скачать PDF» gives a silent PDF next to the .docx; without the WebView2 Runtime (or below 98.0.1108.43) the button
  reports that PDF is unavailable, the .docx stays.
- The PDF differs from the .docx only in typography: Chromium lays it out, the TOC has no page numbers.
- Unsafe COM code is confined to `adapters/pdf` on Windows and tested against the real runtime on the dev machine;
  a WebView2 interface change shows up there.
- Options (a) and (d) are not taken; (c) stays out unless (b) fails in practice.

## Open
- The presentation (.pptx) as PDF: a question to the owner.
- Sources and the page footer: whether the PDF needs anything beyond what the .docx has.
