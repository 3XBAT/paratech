# TASK-165: Photos and scans as note pages without text (core)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-151
- **PRD sections / AC:** ADR-0027 «Handwriting and scans» (option E), «Limits»; ADR-0024; ARCH-44; security review (images)
- **Branch:** ralph/<date>-task-165

## Goal
Photos of handwritten notes can be attached to a lecture now, shown as pages, before any recognition exists.

## Allowed to change
- `paratech/desktop/internal/domain/library/material/{content.go,material.go}` (+ tests): types `jpeg`, `png`, text
  status `no_text` («текст не распознан»)
- `paratech/desktop/internal/adapters/imagepage/**` (new: decode, limits, re-encode)
- `paratech/desktop/internal/cases/notes/**`, `paratech/desktop/internal/cases/library/**` (+ tests)
- `paratech/desktop/internal/input/wails/{notes.go,media.go}` (+ tests): page image by material ID
- `paratech/desktop/pkg/migrations/<next>_material_images.sql` (if the type check is a schema CHECK)

## Out of scope
- Recognition (TASK-167), HEIC (not decodable by the standard library: listed as unsupported).

## Acceptance criteria
- [ ] JPEG/PNG detected by signature; decoded with the standard library under 25 MiB and 40 Mpx (header checked
      before decoding); re-encoded (EXIF/GPS dropped) and the clean bytes are what is stored and shown.
- [ ] Image parts are pages of a note in order; not searched, not cited; the note shows «N стр. без текста».
- [ ] Served to the page by ID only, `image/jpeg`/`image/png`, never SVG; no path crosses the binding.
- [ ] Fixtures: oversized dimensions, truncated file, PNG bomb; fuzz of the decoder wrapper.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of the touched packages and the architecture test pass.
