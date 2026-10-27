# TASK-162 — import recording UI (+ core duration cap for non-WAV)

## Done
- Core (scope widened by the supervisor, from the TASK-161 review medium): every import has a duration in (0, 4 h] taken from headers without decoding. FLAC: STREAMINFO. Ogg Vorbis: rate from the id header, last page granule from a 128 KiB tail. WAV: streamed/zero data size uses the rest of the file. MP3: early refusal only from a Xing/Info/VBRI frame count; the real duration comes from `MP3FrameCounter` fed during the copy; over the cap the copy is deleted. Unknown duration is refused (`ErrAudioDurationUnknown`, «Не удалось узнать длительность записи…»). Fuzz targets for all parsers.
- Early CBR size/bitrate guess for MP3 dropped after review (it could refuse a VBR file without a header that starts and ends in silence).
- `localfile.SeekReader` (seekable original) and `Originals.Open` returns `io.ReadSeekCloser`.
- UI: «Импортировать запись» in «Записи и задачи» and on the subject page; panel with lecture (today's started lessons, or date+time+subject), the note about local recognition, refusals in Russian, row mark «импортирована», estimate and power wait as for other recordings.

## Decisions to confirm
- ADR-0027 (duration "from the WAV header, otherwise unknown") is changed in practice: files whose duration the headers do not give are now refused (WAV with data chunk after 64 KiB, FLAC total 0, chained Ogg, free-format MP3, MP3 with <2 consecutive frames). ADR needs a one-line amendment; Open 4 and Open 10 still unanswered.
- Order in UI is lecture first, then file (the binding opens the native picker itself; the path never comes from the page). AC said file first.
- Header-based cap trusts FLAC/Ogg headers; a lying header passes (needs decoding to catch). MP3 is counted by frames.
- Some MP3 are refused only after the full copy (up to 2 GiB copied, then deleted).
- Extra files: `adapters/localfile/localfile.go`, `app/recordingimport.go`, `input/wails/recordingimport.go`, `frontend/src/bindings/recordingImport.ts`, `styles.css`, `tsconfig.test.json`; test is `test/recordingimport.test.mjs` (runner picks only `test/*.test.mjs`).

## Follow-ups
- «импортирована» mark is session-only (activity `RecordingItem` has no origin); add `origin`/`imported` to the Go activity item and `bindings/activity.ts`, render from it (also needed for TASK-163 rows).
- «Пара из расписания» lists only today's started lessons; no binding for older lessons.
- A11y lows: focus into the panel on open, `aria-controls`, disable the mode switch while busy; re-indent the `rec-start` block in RecordingsPage.
- Verify whisper.cpp decoders size audio by header-reported length; add a note to docs/refs/whisper.md.
- Scan past 64 KiB for the WAV data chunk through the seekable reader; the `noDuration` text says "в WAV или MP3" even for a WAV.
- From TASK-161 still open: orphan copy on crash, duplicate found only after the copy, localfile/doc.go stale.

## Changed existing tests
`recording/audioformat_test.go` (new duration rules, complete synthetic files), `recordingimport/service_test.go` and `fakes_test.go` (seekable fake, counted MP3 duration, virtual 1 GiB WAV, 8 s WAV in the failure test), `wails` error-mapping row and frontend errors list.

## Security
Auditor: no High/Medium. Reviewer: no ARCH blockers; mediums fixed (CBR guess) or recorded above.
