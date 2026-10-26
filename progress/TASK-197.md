# TASK-197 — adapters/pdf: WebView2 PrintToPdf printer (Windows) + stub

## Done
`paratech/desktop/internal/adapters/pdf/`: `webview_windows.go` (Printer, NewPrinter, Print, ErrUnsupported/ErrFailed), `webview2_windows.go` (one print run), `com_windows.go` (own COM code), `win32_windows.go` (runtime lookup, version check >= 98.0.1108.43, hidden window, message loop), `webview_other.go` (stub returning ErrUnsupported), tests. No CGO, no new dependency.
Own hidden WebView2 instance with its own profile folder (dataDir), scripts/web messages/devtools/context menus/host objects off, only the NavigateToString navigation allowed (others and all frames cancelled), HTML up to 2 MB (empty/oversized/NUL -> ErrFailed), PrintToPdf into os.CreateTemp inside dataDir, deleted after, result checked for `%PDF-`. Dedicated locked STA OS thread per call, calls serialized.

## Verification
Real WebView2 tests on the dev machine (runtime 155), go test -race, vet windows/linux, lint v2.14.0 windows/linux, architecture test: green.
Script-off test counts PDF pages (1 vs 5 with scripts forced on in a control run). Navigation test uses a test-only `holdBeforePrint` field (meta refresh to about:blank must not replace a 5-page page; iframe content must not add fonts); verified to fail with the cancels disabled.
Handler IIDs (controller, NavigationStarting, NavigationCompleted, PrintToPdf completed) verified against WebView2.h from github.com/webview/webview_go (module cache only, repo go.mod untouched); other IIDs against Wails sources.

## Decisions to confirm
- Cancellation returns ctx.Err() (Canceled/DeadlineExceeded), not ErrFailed; TASK-198 must map it.
- ErrUnsupported/ErrFailed declared in webview_windows.go and webview_other.go (no untagged file in this task); dedupe into a shared file if TASK-196 adds one.
- A cancelled call can return up to ~5 s late (waits to release the browser so the profile is not locked).
- Real tests use os.MkdirTemp with a retrying cleanup (browser process holds the profile briefly after release; t.TempDir cleanup is too short).

## Follow-ups
- TASK-198: pass a dataDir exclusive to this Printer (not the Wails profile); no second Printer on it; set a ctx timeout (no ProcessFailed handler, a crash is noticed at the deadline).
- Subresource network loads are blocked only by the HTML's own CSP (TASK-196 must always emit `default-src 'none'`); optional hardening: WebResourceRequested filter in the adapter.
- WEBVIEW2_* env/registry overrides are not neutralised in the adapter (Wails clears them process-wide); not neutralised in isolation.
- arm64 keeps runtime default margins.
- Leftover temp folders from early dev runs in %TEMP% (paratech-pdf-test-2681873220, TestPrintTimeoutThenRecovers2620048108) — owner may delete.
- ADR-0020 status/option (b) is recorded by TASK-196.

## Changed existing tests
None (TestOpErrorKeepsKind is new in this task).

## Security
Auditor: no high/medium; lows L1 (CSP-only egress block), L2 (PDF may be left in dataDir after a cancelled print until next Print sweeps it). Reviewer medium findings (navigation test, NavigationId hang, HRESULT truncation, docs) fixed.
