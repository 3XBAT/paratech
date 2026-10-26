# TASK-197 (PAUSED, WIP)

Done: implementation committed (406f0efe) in paratech/desktop/internal/adapters/pdf (webview_windows.go, webview2_windows.go, com_windows.go, win32_windows.go, webview_other.go, tests). Real WebView2 tests, vet and lint (windows/linux) pass; architecture test passes.

Left: reviewer + security-auditor (parallel), fix findings, tasks.json status, final progress file, docs/qa/tasks/TASK-197.md, fetch/merge master (TASK-196 html.go in same package), push, PR, audit-pr.sh.

Notes for the final file: ErrUnsupported/ErrFailed declared in both webview_windows.go and webview_other.go (move to a shared file if TASK-196 adds one); cancellation returns ctx.Err(), not ErrFailed (TASK-198 must handle); a cancelled call can return up to ~5 s late (waits for browser release); arm64 keeps default margins; no test for meta-refresh/iframe navigation blocking; script-off test counts PDF pages (1 vs 5 with scripts on); leftover temp folders paratech-pdf-test-* and TestPrintTimeoutThenRecovers* in %TEMP%.
