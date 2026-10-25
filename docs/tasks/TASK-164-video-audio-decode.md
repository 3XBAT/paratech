# TASK-164: Video and M4A/AAC/WMA recordings: local audio extraction (BLOCKED)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-161; **owner decision ADR-0027 Open 4**
- **PRD sections / AC:** ADR-0027 «Old recordings»; hard rules 1, 2, 9; ARCH-33
- **Branch:** ralph/<date>-task-164

## Goal
A lecture saved as video (MP4, MKV, WEBM) or as M4A/AAC/WMA is turned locally into a track whisper-cli reads.

## Blocked
Needs the owner's choice: Windows Media Foundation through COM syscalls (proposed; as the WASAPI adapter, no CGO,
built into Windows, Windows N needs the Media Feature Pack), a bundled ffmpeg (size, licence) or «convert it
yourself». This draft assumes Media Foundation; rewrite it if another option is chosen.

## Allowed to change
- `paratech/desktop/internal/adapters/mediadecode/**` (new; `_windows.go` + `_other.go` stub)
- `paratech/desktop/internal/cases/recordingimport/**` (a decode port)
- `paratech/desktop/internal/app/**`; `docs/refs/mediafoundation.md` (facts from Microsoft docs)

## Acceptance criteria
- [ ] Source reader decodes the first audio stream to 16 kHz mono PCM WAV in the private run directory, streamed, with
      cancellation and a time budget; video frames never decoded to disk.
- [ ] Missing Media Foundation (Windows N) is reported «нужен Media Feature Pack», no crash.
- [ ] Windows tests on small fixtures (mp4/aac, m4a, webm/opus if supported), the stub elsewhere; no CGO check passes.
