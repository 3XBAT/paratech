# TASK-177 — LibraryService.OpenMaterial (open in Word / PowerPoint)

## Done
- `LibraryService.OpenMaterial(ctx, materialId)` in `input/wails/library.go` (+ `library_open.go`): only an ID comes in; result `{status, message}` with status opened | not_found | refused | failed and fixed Russian texts. Logs carry the material ID and a result code only.
- New adapter `adapters/fileopen` (windows build tag + stub): path built only from an artifact ref; Clean/EvalSymlinks root, per-component Lstat (no junctions/symlinks), extension allowlist (.docx .pptx .txt .md .pdf), drive-letter paths only, explorer.exe from GetWindowsDirectory, single argument, no shell.
- App wiring (`app/core.go`), `frontend/src/bindings/library.ts` `openMaterial(id)` with fixed texts per status.
- Blobs are stored as `materials/<sha256>` without extension, so a named copy `exports/library/<id>/<name>.<ext>` is made once (never overwritten) and opened.

## Decisions to confirm
- Edits made in Word stay in the copy and do NOT flow back into the library material; "Отправить преподавателю", summaries etc. read the original blob. The UI (TASK-132/106) should say the file opens as a copy; follow-up: re-import an edited copy as a new version.
- Copy name is `<cleaned original name>.<ext>` (ext from the content-detected type), differing from ADR-0024 (`<material-id>.<ext>`); ADR not amended (not in scope) — amend in one line or revert.
- "opened"/"failed" texts are our choice.

## Follow-ups
- Reviewer medium: the copy/naming policy and the `MaterialViews` port live in input/wails (ARCH thin-input); better moved into the adapter or a `cases/library` use case.
- Copies under `exports/library/<id>/` are not purged when a material is removed.
- `.md` is stored as TXT so it opens as .txt; `.md` in the allowlist is unreachable (harmless).
- A data path containing `,` `;` `"` makes every open refused (fail-closed).
- core.go builds the artifact root twice; reuse a single value.
- Hard link / TOCTOU need same-user write access to the data dir; not addressed.

## Changed existing tests
- `input/wails/shell_test.go` `validCore`: added `MaterialViews` and `Opener` fakes (OpenServices now requires them).

## Security
Auditor: no high/medium findings; two lows (allowlist wider than needed, hard links). Reviewer: approve, 3 medium (above), recorded not fixed.
