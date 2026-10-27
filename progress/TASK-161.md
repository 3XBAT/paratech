# TASK-161 — import of an audio file recorded elsewhere (core)

## Done
- Domain `recording`: `Origin` (captured/imported), `NewImported` (COMPLETED at once, startedAt = chosen lecture time, lesson link or snapshot), capture/recovery transitions refused, `DetectAudio` by content (RIFF/WAVE with header duration, fLaC, ID3/MPEG frame, Ogg Vorbis only). Limits as constants: 2 GiB, WAV 4 h, 256 MiB free-space reserve.
- Migration 0042 `recordings.origin` + `source_sha256` (partial unique index); mirrored in `docs/contracts/schema.sql`; sqlc regenerated.
- Use case `cases/recordingimport`: read-only open of the original, atomic copy to `recordings/<id>.<ext>` through the Storage Manager with sha256 on the way, same bytes -> same recording, then the usual STT enqueue (pinned model, power policy). No Hub port.
- Binding `RecordingImportService` (native picker, path never from the page), `bindings/recordingImport.ts`. `docs/architecture.md` §12: one line.
- Deleting (ADR-0018) removes only the copy; tested with a temp original.

## Decisions to confirm
- ADR-0027 Open 4 (video/M4A) and Open 10 (limits) not answered: only whisper-decodable formats; Opus/AAC/M4A/WMA refused; limits are constants (2 GiB, 4 h, 256 MiB reserve).
- Re-import of bytes whose recording is being deleted -> `ErrDeleting` ("try in a minute").
- `Recording.Duration()` is "unknown" when duration is 0 (captured always >0; negligible).
- Files outside the draft list: `input/wails/shell.go`, `shell_windows.go`, `shell_test.go` (binding registration), `docs/contracts/schema.sql` (required by the schema test).

## Follow-ups
- Medium (reviewer): non-WAV files have no duration cap before STT; a 2 GiB low-bitrate MP3/Ogg/FLAC may be tens of hours and whisper-cli decodes it to RAM. Estimate duration from MP3 bitrate, FLAC STREAMINFO, Ogg granule.
- Orphan `recordings/<id>.<ext>` if the app crashes between copy and row save; no startup sweep.
- Duplicate detected only after the full copy (2 GiB copy then delete; free-space check may fail first).
- Move `CheckImportSpace`/limits to `services/limits` when TASK-163 reuses them.
- `adapters/localfile/doc.go` still says model files only.
- whisper-cli decoders (miniaudio, dr_mp3, dr_flac, stb_vorbis) now see foreign files; sidecar is per-job and unprivileged; keep whisper.cpp current.
- `npm run build/test` not run (typecheck only); UI is TASK-162.

## Changed existing tests
`sqlite/migrate_test.go`: newest migration is 42 (`recordingOriginVersion`), comment fixed. `wails/shell_test.go`: `validCore()` gets `RecordingImport` fake (OpenServices requires it).

## Security
Auditor: no High/Medium findings. Reviewer: approve, no blockers.
