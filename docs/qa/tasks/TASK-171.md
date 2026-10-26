# QA TASK-171: thread no longer stuck busy

Core only (UI for a stuck request is TASK-143).

1. `go -C <repo>/paratech test -race ./desktop/internal/cases/agentturn/... ./desktop/internal/input/agent/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/...` (architecture test) passes.
3. Read `stuck_test.go` in both packages: Submit/base/save errors and panics leave the request STOPPED and the next Ask passes; a running request without a loop is recovered once on Ask/Rerun; with a live loop (hung tool) the answer stays busy until «Стоп».
4. In the app: ask a question in a thread, then another right after the answer — it must be accepted. If «Предыдущая просьба ещё выполняется» ever shows again, keep desktop.log and the database and tell the developer (root cause is not proven).
