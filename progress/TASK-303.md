# TASK-303 — recording import core: orphan sweep, duplicate before the copy, localfile doc

Done together with TASK-302 (one branch, one PR).

## Done
- Sweep at start-up: `Service.SweepOrphans` (cases/recordingimport/sweep.go) deletes through the Storage Manager a file of `recordings/` only if its name is `imp-<id>.<wav|flac|mp3|ogg>` (round-trips through `recording.ImportedTrackRef`), it is older than 10 minutes (Clock port) and `Recordings.Get(id)` says not found. A lookup error keeps the file; directories are skipped; one failure does not stop the others. Holds the import mutex. Wired as one start-up cleanup in `app/recordingimport.go`, failure only logged.
- Imported recordings are now named `imp-<generated id>`, so their copy `recordings/imp-<id>.<ext>` is told apart from capture tracks (`<id>.wav`; generated ids are base32 without a hyphen). Copies made by earlier builds keep their names and are never swept.
- Duplicate before the copy: the original is streamed once for sha256 (MP3 frames are counted on the same pass, so over-long/uncountable MP3 is refused before any copy); a known digest returns the same recording with no id, no free-space check and no write; otherwise the copy is made with sha256 on the way and must equal the first digest, else the copy is deleted and `ErrChanged`.
- `adapters/localfile/doc.go` describes `Reader`, `SeekReader` and `Originals.Open` (read-only `io.ReadSeekCloser`).

## Decisions to confirm
- The `imp-` id prefix rule lives in the use case (sweep.go); it could move to `domain/recording` next to `ImportedTrackRef` (follow-up).
- Files outside the task list: `adapters/filestore/store.go` (+doc.go, test): new `ListKind(kind)` and `ModTime(ref)` (no ArtifactRef can name the `recordings/` directory; the Store gave no times), `List` shares a `names()` helper.
- A new file is now read twice (digest, copy) in exchange for no second 2 GiB copy of a duplicate.

## Follow-ups (lows)
- Free-space check could run before the digest pass when no duplicate is expected.
- Low (auditor): header/tail/WAV chunk headers are read before the digest pass; comparing the first `len(head)` bytes in `digestOf` with `head` would close a rewrite-in-between of the user's own file (only gain: a longer job).
- Mode-switch busy reason text has no separate line (the button label explains).

## Changed existing tests
`recordingimport/fakes_test.go` (`seqIDs` returns bare numbers because the service adds `imp-`; `memStore` write counter, `ListKind`, `ModTime`; `memRecordings.Get`), `TestImportDurationCap` (MP3 over the cap is refused before any copy), `TestImportWAVDataPastTheHead` (bytes read: the file twice), `TestImportOfTheSameBytesTwice` (one write, no delete, third import with zero free space).

## Security
Auditor: no High/Medium; sweep cannot match capture files, links are skipped, race with an import covered by the shared mutex, TOCTOU of the source handled by the digest check. Reviewer: approve.
