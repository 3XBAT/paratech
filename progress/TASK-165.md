# TASK-165 — photos and scans as note pages without text (core)

## Done
- `material`: types jpeg/png (by signature), text status `no_text`, invariant "photo <=> no_text", limits `MaxImageBytes` 25 MiB, `MaxImagePixels` 40 Mpx (ADR Open 10 not answered: constants). HEIC/AVIF/GIF/WebP/BMP/TIFF/SVG refused (`ErrUnsupportedImage`).
- `adapters/imagepage`: stdlib only. Size, then header (`DecodeConfig`), memory estimate and "too small for its pixels" bomb check before decode; decode, EXIF orientation applied (own bounded parser), re-encode (JPEG q90 / PNG) so EXIF/GPS/ICC/text chunks are gone. Fixtures: huge header, truncated, PNG bomb, bad CRC; `FuzzClean` (1.0M runs, no failure).
- `cases/library`: port `PageImages`; `Import` stores only clean bytes, dedupe on clean digest; no cleaner => photos refused. `MaterialText` refuses non-readable types, `Inspect` returns `no_text`, `PageImage(id)`. `cases/notes`: `PageImage`, `View.NoTextPages`.
- Binding: `/media/notepage/<material id>` (content type from the stored type only, nosniff, no-store, CSP sandbox, GET/HEAD); `NotePart.PageURL`, `NoteView.NoTextPages`; photo extensions in the notes drop allow-list and open dialog (image part only; `.md` still missing). No path crosses the binding.
- Migration 0041 rebuilds `materials` (CHECKs on type/text_status), NO TRANSACTION; renumbered from a placeholder after merging master (0040 = agent chats). TASK-230 may take 0041 too: renumber at push if so.
- No recognition, no PageReader, nothing to the model or the Hub.

## Decisions to confirm
- Photos pass through the general `Import`, so «Входящие» picker also accepts them (library list shows «текст не распознан», photo error texts reused); library dialog/drop filters still list documents only.
- `/media/notepage` serves any active photo by ID, not only note parts (needs a repository query otherwise).
- SVG text is now refused everywhere.
- PNG eXIf orientation ignored (JPEG EXIF only).

## Follow-ups
- Frontend: render `pageUrl` and `noTextPages`, add the three photo texts to `bindings/notes.ts` (TASK-154 area).
- Progressive JPEG under 40 Mpx may exceed the decoder memory cap (~21 Mpx effective) and says "25 MB or 40 Mpx": needs a dedicated error through domain/cases/bindings.
- Migration Down is lossy: leaves `summaries.material_id`/`assignments.material_id` dangling (FK off), possible gaps in `note_parts.position`; use NULL updates + foreign_key_check.
- Test `TestMigrationMaterialImagesKeepsReferences` checks `foreign_keys` through the pool, not the migration's own connection.
- `orient` is per-pixel interface calls (seconds for 40 Mpx rotated); `NoTextPage()` predicate could live in the aggregate; `.md` in dialog/drop.

## Files outside the task's list (small, needed)
`cases/homeworkflow/material.go` (photos refused like PDFs), `input/agent/tools.go` (`no_text` => not_read), `input/wails/{library.go,library_open.go,librarydrop.go,notespicker_windows.go,shell_windows.go}`, `app/core.go`, `adapters/sqlite/{migrate.go,migrate_test.go}`, `docs/contracts/schema.sql`.

## Changed existing tests
All additive rows/fakes: `material_test.go`, `text_test.go`, `cases/library/fakes_test.go`, `cases/notes/fakes_test.go`, `input/wails/notes_test.go`, `library_test.go`, `input/agent/material_test.go`, `homeworkflow/material_test.go`, `sqlite/migrate_test.go` (hard-coded version -> `materialImagesVersion`).

## Security
Auditor: no findings. Reviewer: approve; medium (library display of photos) fixed in a5cb5d96; lows above.
