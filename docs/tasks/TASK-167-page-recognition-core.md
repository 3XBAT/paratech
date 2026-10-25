# TASK-167: Recognition of photo and scan pages (core) (BLOCKED)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-165; **owner decision ADR-0027 Open 3**
- **PRD sections / AC:** ADR-0027 «Handwriting and scans»; ADR-0022; hard rules 3, 5, 9
- **Branch:** ralph/<date>-task-167

## Goal
Pages without text get recognized text the student can check, which then counts as their notes.

## Blocked
The engine is the owner's decision: Windows OCR (local), Tesseract sidecar (local), or a vision model through the Hub
(images leave the PC; opt-in, cost). This task is the engine-independent part plus one engine adapter.

## Allowed to change
- `paratech/desktop/internal/cases/noterecognition/**` (new: port `PageReader`, job `note_ocr`)
- `paratech/desktop/internal/domain/library/material/**` (statuses `recognizing`, `recognized`, provenance)
- `paratech/desktop/internal/adapters/<engine>/**` (one, per the decision)
- `paratech/desktop/internal/input/wails/notes.go` (+ tests), `paratech/desktop/internal/app/**`

## Acceptance criteria
- [ ] Job one page per queue step, idempotent per page digest and engine version; cancellable; power-aware if local.
- [ ] Recognized text has page locators and provenance «распознано»; it is searchable and citable only after the
      student opens and accepts (or edits) it.
- [ ] With the Hub engine: per-import opt-in with a privacy notice and a cost estimate; nothing is sent without it.
- [ ] Recognized text is untrusted data (injection fixture).
