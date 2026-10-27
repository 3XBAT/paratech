# TASK-254 — .md in the open dialog and the drop allow-list

## Done
- `.md` and `.markdown` added to the open-dialog pattern (`librarypicker_windows.go`, shared with the notes dialog) and to the explicit `droppedExtension` switch (`input/wails/library.go`).
- Content-based detection unchanged: Markdown is TXT by content (new `TestDetectType` case).
- Tests: `TestDroppedExtension`, `TestLibraryDropImportsMarkdown`, new rows in `TestLibraryDropRefusesPaths`.

## Decisions to confirm
- `ErrLibraryUnsupported` text unchanged (Markdown is UTF-8 text; the same text is copied in the frontend bindings).
- Dialog label lists `*.md` only; the pattern also accepts `*.markdown`.

## Follow-ups
- `adapters/fileopen` allowedExtensions has `.md` but not `.markdown` (probably irrelevant: opened copies are named by content type).

## Changed existing tests
None (only added cases).

## Security
Auditor: nothing found. Reviewer: approve, no blockers.
