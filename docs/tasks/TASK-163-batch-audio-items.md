# TASK-163: Audio files in a bulk import become imported recordings

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-159, TASK-161
- **PRD sections / AC:** ADR-0027 «Bulk import», «Old recordings»; hard rule 2
- **Branch:** ralph/<date>-task-163

## Goal
A folder with notes and old lecture audio is imported in one review: audio rows become recordings of the matched
lecture.

## Allowed to change
- `paratech/desktop/internal/domain/library/importbatch/**` (item kind `recording`)
- `paratech/desktop/internal/cases/notesimport/**` (a port implemented by `cases/recordingimport`, wired in `app`)
- `paratech/desktop/internal/cases/recordingimport/**` (method the port needs)
- `paratech/desktop/internal/input/wails/notesimport.go` (+ tests), `paratech/desktop/internal/app/**`

## Out of scope
- UI changes beyond what TASK-160 already renders (the kind column).

## Acceptance criteria
- [ ] Audio types of TASK-161 are staged as kind `recording`; matching as for notes (date and time from the name).
- [ ] Commit step of an audio item calls the recording import once (idempotent by sha256); the transcribe jobs queue
      behind each other and respect the power policy.
- [ ] Tests on fakes and one scenario test; `go vet`, `go test -race` and the architecture test pass.
