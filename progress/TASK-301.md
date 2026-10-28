# TASK-301 — WAV data chunk beyond 64 KiB; "duration unknown" text names the format

## Done
- WAV: when the 64 KiB head ends before the `data` chunk, the domain tells the use case where the next chunk header is (`ImportedAudio.WAVChunkAt`, `WAVChunk`, 8 bytes per step); the use case seeks, reads 8 bytes, and seeks back to the end of the head before the copy. At most 64 header reads, chunk bodies are never read, odd sizes are padded, a chunk running past the file or a short header ends the scan (refusal). Duration rules are unchanged.
- `AudioDurationUnknownError{Format}` (still `errors.Is(ErrAudioDurationUnknown)`); the wails binding picks a Russian text per format (WAV/MP3/FLAC/OGG), generic text as fallback; the same four texts in `bindings/recordingImport.ts`.
- ADR-0027 TASK-162 amendment edited: bounded chunk scan, refusal text names the format.
- Tests: JUNK 100 KiB before data, 63/64 chunks bound, no data, chunk past the file, bytes-read counters, per-format texts; new fuzz `FuzzWAVChunkWalk`; `FuzzDetectAudio` extended (20 s runs clean).

## Decisions to confirm
- Domain stays IO-free: step-wise API driven by the use case (like `NeedsFrameCount`/`Counted`) instead of `io.ReaderAt`. The use case therefore does more than "pass the format" (`walkWAVChunks`).
- Files beyond the task list: `domain/recording/recording/errors.go`, `mp3.go` (`Counted` refuses an unfinished walk), `audioformat_wav.go` (new, WAV code moved out of `audioformat.go`).
- A `fmt ` chunk after a large chunk past the head (or crossing the end of the head) is still refused as "duration unknown": the walk reads headers only.

## Follow-ups
- The walk does not check `ctx` between its at most 64 reads (negligible).
- From TASK-161/162 still open: orphan copy on crash, duplicate found only after the copy, localfile/doc.go stale, whisper.md note.

## Changed existing tests
`audioformat_test.go`: row "wav data past the head" now uses a head-sized file (the 1 TiB case is covered by the new walk tests); new row "wav without fmt before a chunk past the head"; `FuzzDetectAudio` invariant allows a pending WAV walk. `fakes_test.go`: bytes-read counter.

## Security
Auditor: nothing found (no overflow, bounded reads, seek-back correct). Reviewer: approve, no blockers; the missing bookkeeping files it flagged are this file and the QA file.
