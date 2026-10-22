# TASK-124: navigation of 6 icons, Предметы, drop zone, Журнал

Done: Go bindings (`input/wails/agentjournal.go`: History with filter over agentjournal.List, 90 days; Undo; UndoRequest; `LibraryService.ImportDroppedFile`) and frontend (6-icon rail with tooltips, Настройки tabs, accent picker, Предметы, Журнал, drop overlay, panel Undo / «Отменить всё»). Предметы was already bound in library.go.

Owner request (2026-10-22): the panel «Отправить» is an up-arrow icon button (accent, aria-label «Отправить», Enter still sends).

## Partial: AC4 (drop zone)
- Native file drop is NOT wired: WebView2 gives JS no path. Needs Wails `EnableFileDrop` + `events.Common.WindowFilesDropped` in `shell_windows.go` emitting `files:dropped` (string[]), checked on real Windows. Until then every real drop shows the «Добавить файл» hint.
- Drop does not create an agent request with an upload (no such use case; `Ask` has no upload ids). The file only goes to «Входящие».
- Reviewer High 1: `ImportDroppedFile(path)` is page-supplied path surface; safer design is Go-only native drop with only ids crossing. Path check is a drive-letter-only allow-list (auditor F1 fixed). Decision for supervisor.

## Decisions to confirm
- History takes a filter (was a subject id); reads the newest 200 lines then filters (Low: older lines within 90 days can be cut).
- New enums: refusal `not_undoable`, `busy` (version conflict); problem `busy`.
- Send email line says "sent" only when the job is COMPLETED.
- Panel thread history (old `History(subjectId)`) is gone; needs a thread-history read in cases/agentturn.
- «Сегодня» is a placeholder mounting the old recordings/tasks page; TASK-125 must keep or relocate it.

## Follow-ups
- UNDOING left after name_taken refusal in the core (reviewer Low 4); popup window ignores the accent; removing/moving subjects and files not built; filters pushed into agentjournal.List.
- Architecture: state that native drop paths stay in Go.

## Changed existing tests
Go: agent_test (newAgent, stub test renamed), shell_test, settings_test. Frontend: agent.test, activity.test, nav.test (rewritten).

Review: first round changes requested (drop gap, send_email status, busy); fixed; re-review approved. Auditor: Medium F1 (mixed-separator UNC) fixed.
