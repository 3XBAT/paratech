# TASK-222 — Recognition: skip silence with VAD (Silero) and an honest time estimate

## Done
- whisper-cli runs with `--vad -vm <component>\ggml-silero-v5.1.2.bin` (flags checked in `--help` of the pinned b5454 build). Beam/best-of, threads, the 40 % cap and `loadpolicy.go` are untouched.
- The VAD model ships as a separate file of the speech component: a file in `speechcomponent.json` may have its own `source_url` (downloaded on its own, size and SHA-256 checked, host added to the allowlist), key `vad_model`. Installer (NSIS) and `build-alpha.ps1` ship it too. ADR-0031 (new), ADR-0016 amended.
- `speedestimate`: table recalibrated on the 2026-10-26 measurements, then x0.6 for skipped silence. Migration 0037 clears `stt_speeds` (data only).
- Time codes: starts stay on the recording time line; a segment before a pause ends where the next speech starts. The transcript view's paragraph break now caps a segment end by what the text can take to say (review finding: pauses had stopped starting paragraphs). Fixture `adapters/whispercli/testdata/vad` and test `TestVADFixtureKeepsTheTimeline`.
- Docs: `docs/refs/whisper.md`, `docs/refs/packaging.md`.

## Real runs (synthetic audio, base-q5_1, 4 threads, via heavy.sh)
| Sample | no VAD | VAD |
|---|---|---|
| English, 217 s, long pauses | 29.3 s | 14.8 s |
| Russian, 279 s, 74 % speech | 57.0 s | 53.7 s |
| research lecture sample, small-q5_1 | 239.5 s | 120.9 s |

## Decisions to confirm
1. The VAD model is a required file of the component: a component installed by an older version (13 files) shows as damaged until reinstalled (~12.5 MB). Alternative: optional file, download only it.
2. The estimate table was recalibrated (about 3.3x larger per family) and then x0.6; shown times grow about 2x, more models get the "heavy" flag. ADR-0016 example numbers are marked as old.
3. Frontend (`transcriptView.ts`, `test/transcript.test.mjs`) touched outside the task's named files, for the paragraph break.

## Follow-ups
- Pin the VAD `source_url` to a commit of `ggml-org/whisper-vad` and compare SHA-256 with `lfs.oid` (no network was allowed); the HF redirect hosts are unverified.
- `makensis` packaging not verified (NSIS not installed).
- Go parser adds a separate file's host to the allowlist automatically; `build-alpha.ps1` requires it in `download_hosts` (shipped manifest lists it).
- `audiocapture` `TestRecorderCheckLevels/fail` flaked once under load in the full run (untouched package).

## Changed existing tests
`whispercli/args_test.go` (buildArgs gained vadModel), `whispercli/fake_test.go`, `app/speech_test.go` TestEmbeddedSpeechManifest (VAD file and HF hosts), `sqlite/migrate_test.go` (version 37), `speedestimate_test.go`, `cases/sttspeed/service_test.go`, `input/wails/load_test.go` (numbers follow the new table).

## Review and security
Reviewer: 2 blockers (migration 0035 and ADR 0030 numbers taken on master: renumbered to 0037 / 0031), 1 high (paragraphs, fixed in the frontend with a test), mediums recorded above. Security auditor: no high/medium findings.
