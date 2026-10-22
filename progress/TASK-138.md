# TASK-138: real file drop, paths stay in Go

Done: `EnableFileDrop` on the main window and an `events.Common.WindowFilesDropped` listener in `input/wails/shell_windows.go`; logic in `librarydrop.go` (no build tag): the drive-letter allow-list of TASK-124 per path, import through the library use case, max 10 files per drop, drops serialised by a mutex, nothing emitted after shutdown. The page gets the event `library:dropped` with `{error, files:[{name, existing, material, error}]}` and never a path. The `ImportDroppedFile(path)` binding is removed. Frontend: `DropZone` shows the per-file report; `data-file-drop-target` is set on `document.body` (App.tsx untouched, parallel task). `docs/architecture.md`: one bullet in §6.

## Wails beta.29 facts (checked in the module source)
- On Windows the runtime reads the dropped paths in Go, sends them to the page JS, and the JS sends them back (window method 50, `FilesDropped`), which fires `WindowFilesDropped`. So "paths never leave Go" is not literal: our bindings/events carry none, the Wails runtime does. A page script can forge a drop with any path list (also from the popup). Residual risk, same as the removed binding; the allow-list and the regular-file-only reader are the defence.
- A drop is accepted only if `elementFromPoint().closest('[data-file-drop-target]')` hits at dragenter, dragover and after the Go round trip. The first version put the attribute on the overlay (pointer-events none, unmounted on drop): reviewer BLOCKER, fixed by putting it on body.

## Decisions to confirm
- Event renamed `files:dropped` -> `library:dropped`; more than 10 files refuse the whole drop.
- `app.Event.Emit` reaches all windows; the popup ignores it.

## Follow-ups (reviewer lows)
- Tray Quit does not cancel the `run` context: cancel a drop context in `app.OnShutdown`; `dropOutcome` should stop its loop on ctx cancel (add a test).
- Typo `const enter =(e` in DropZone.tsx:55.
- A drop on the document scrollbar may be refused (edge).
- Dropped file is not turned into an agent request with an upload (no use case yet).

## Changed existing tests
Go: library_test (ImportDroppedFile test replaced by the drop-handler tests), spec_test, shell_windows_test, TestLibraryDropImports (dropped the fake's Type assertion). Frontend: drop.test.mjs rewritten, library.test.mjs (binding gone; errorTexts gets the 10-files text).

Review: first round changes requested (blocker: drop target); fixed; re-review approved. Security: no High/Medium; forged-drop residual risk accepted (see above).
