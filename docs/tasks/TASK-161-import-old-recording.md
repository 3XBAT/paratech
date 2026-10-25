# TASK-161: Import an old audio recording of a lecture (core and binding)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** -
- **PRD sections / AC:** ADR-0027 «Old recordings»; PRD §6.2, §7.4, §8; AC-06, AC-08, AC-21; ADR-0016, ADR-0018; hard rules 2, 5
- **Branch:** ralph/<date>-task-161

## Goal
An audio file of a lecture recorded elsewhere becomes a recording of that lecture and goes through the existing local
transcription and everything after it.

## Allowed to change
- `paratech/desktop/internal/domain/recording/recording/**` (`NewImported`, origin)
- `paratech/desktop/pkg/migrations/<next>_recording_origin.sql`
- `paratech/desktop/internal/adapters/sqlite/{recordings.go,queries/recordings.sql,sqlcgen/**}` (+ tests)
- `paratech/desktop/internal/adapters/filestore/**` (copy-in of a track, if a method is missing)
- `paratech/desktop/internal/cases/recordingimport/**` (new)
- `paratech/desktop/internal/input/wails/{recordingimport.go,recordingimport_test.go,recordingpicker_windows.go,recordingpicker_other.go}`
- `paratech/desktop/internal/app/**`, `paratech/desktop/frontend/src/bindings/recordingImport.ts`
- `docs/architecture.md` §12 (`cases/recordingimport`)

## Out of scope
- Video and other containers (TASK-164), batch (TASK-163), UI (TASK-162).

## Acceptance criteria
- [ ] `recording.NewImported(spec, now)`: COMPLETED at once, origin `imported`, `startedAt` = the lecture's date and
      time chosen, lesson link or snapshot (subject, kind, title) from the anchor; capture-only transitions refused.
- [ ] Type from content (RIFF/WAVE, fLaC, ID3/MPEG frame, OggS), only what whisper-cli v1.9.5 decodes
      (`docs/refs/whisper.md`); ≤ 2 GiB; WAV duration from its header (≤ 4 h), else unknown until the transcript.
- [ ] The file is copied into `recordings/<id>.<ext>` through the Storage Manager (atomic); then the normal transcribe
      job is enqueued (power-aware, model pinned). Nothing is uploaded (AC-06): test that no Hub port is used.
- [ ] Same bytes twice = the existing recording (sha256 kept on the recording or the import row).
- [ ] Deletion per ADR-0018 removes the copy only; the original file is never touched (test with a temp file).
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of the touched packages and the architecture test pass.
