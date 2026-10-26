# TASK-177 — owner's check

The binding has no button yet (UI tasks 132/106/107/134). Automated tests cover everything except the real launch.

1. Run `go test -race ./desktop/internal/adapters/fileopen/... ./desktop/internal/input/wails/...` from `paratech/`: green.
2. Real check (when a UI button exists, or via a debug call): import a .docx into the library, call `OpenMaterial(id)`. Expected: Word opens `exports/library/<id>/<name>.docx` in the data folder, status `opened`.
3. Same for a .pptx (PowerPoint) and a .pdf.
4. Delete the copy file and the original blob, call again: status `not_found`, message «Файл не найден: возможно, его удалили.»
5. Edit and save the copy in Word, call again: your edits are still there (the copy is not overwritten); the library material itself is unchanged.
