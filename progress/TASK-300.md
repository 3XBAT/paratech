# TASK-300 — «импортирована» mark survives a restart

## Done
- Go: `activity.Recording.Origin` (from `Recording.Origin()` in the use case) and `RecordingItem.origin` ("captured" | "imported"; empty or unknown -> "captured") in the Wails activity item. The sqlite read path already filled the origin (migration 0042), so no adapter change and no migration.
- Frontend: `bindings/activity.ts` parses `origin` (anything but "imported" is captured); `RecordingsPage` passes `imported={r.origin === "imported"}`; the session `importedIds` state and `withImported` are removed; after an import the page refreshes and the mark comes from the reloaded data.
- Tests: Go for both values (`TestListRecordingsOrigin`, `TestOriginText`, JSON test of the item), frontend for parsing both values and a reloaded list.

## Decisions to confirm
None.

## Follow-ups
- Low (reviewer): the frontend test repeats the page's `r.origin === "imported"` expression rather than rendering the page; moving the rule into `RecordingRow` (drop the `imported` prop) would keep it in one place.
- `RecordingRow` is used only by `RecordingsPage`; TASK-163 rows can use the same field.

## Changed existing tests
`cases/activity/service_test.go` (expected rows carry `Origin`), `wails/activity_test.go` (exact JSON has `origin`), `frontend/test/activity.test.mjs` and `lecturetext.test.mjs` (fixtures get `origin: "captured"`, the parser always returns it), `frontend/test/recordingimport.test.mjs` (the `withImported` test is removed with the function).

## Files outside the task list
`frontend/src/recordingImportView.ts` (removed `withImported`), `frontend/src/pages/RecordingImportPanel.tsx` (comment), `cases/activity/types.go` and `service.go` (the field was not in the cases row).

## Security
Auditor: nothing found. Reviewer: approve, no blockers.
