# S-002: System audio capture spike

- **Agent:** go-core (+ user verifies on Windows)
- **Label:** spike
- **Depends on:** T-001
- **PRD sections / AC:** §6.2, §6.3, AC-03, AC-05, AC-24
- **Branch:** task/S-002-audio-capture

## Goal
Pick how to capture system audio (WASAPI loopback) on Windows from Go: `malgo` (CGO), a pure-Go WASAPI binding, or a small recorder process.

## Allowed to change
- `desktop/spikes/audio-capture/` (throwaway), `docs/adr/` (result), `docs/refs/audio.md`

## Acceptance criteria
- [ ] Working prototype for each viable option that records system audio to chunked WAV/FLAC for at least 10 minutes
- [ ] Comparison: reliability, CPU use, build complexity (CGO impact on CLAUDE.md rule 1), behavior when output device changes, crash-recovery friendliness (chunks readable after a kill)
- [ ] Recommendation written as an ADR; user verifies on Windows with a real online call
