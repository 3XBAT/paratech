# TASK-158: Folder and zip source for bulk import (adapter)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** -
- **PRD sections / AC:** ADR-0027 «Bulk import», «Limits»; ARCH-31, ARCH-33; hard rule 5; security review (paths)
- **Branch:** ralph/<date>-task-158

## Goal
Read the files of a folder the student picked (an Obsidian vault, an unzipped Notion export) or of a `.zip` safely and
within limits, without ever writing there.

## Allowed to change
- `paratech/desktop/internal/adapters/notesource/**` (new)
- `paratech/desktop/internal/domain/library/values/**` (an entry value type, if the port needs one; + tests)
- `docs/architecture.md` §5 adapter list (the name only)

## Out of scope
- The use case and its port (TASK-159).

## Acceptance criteria
- [ ] Walk: depth ≤ 8, ≤ 5000 entries looked at, ≤ 500 files kept, ≤ 2 GiB in all; hidden entries, `.obsidian`,
      `.trash`, `.git`, `node_modules` skipped; reparse points (symlinks, junctions, OneDrive placeholders not
      downloaded) are not followed and are reported; read-only opens only.
- [ ] Zip (`archive/zip`): ≤ 1 GiB unpacked, ratio ≤ 100:1 per entry, entry names cleaned (no `..`, absolute or drive
      paths, NUL), nested zips not opened.
- [ ] Each entry: relative path (display and hints only), size, a reader; type is decided later from content.
- [ ] Windows tests on real junctions and a temp directory; other OSes build (stub where needed).
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of the package and the architecture test pass.
