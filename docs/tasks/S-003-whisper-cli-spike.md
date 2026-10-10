# S-003: whisper-cli sidecar spike

- **Agent:** go-core (+ user verifies on Windows)
- **Label:** spike
- **Depends on:** T-001
- **PRD sections / AC:** §8, AC-06, AC-07, AC-08
- **Branch:** task/S-003-whisper-cli

## Goal
Verify the sidecar approach: Go starts `whisper-cli` for one audio file, tracks progress, can cancel, and reads a structured result.

## Allowed to change
- `desktop/spikes/whisper-cli/` (throwaway), `docs/adr/`, `docs/refs/whisper.md`

## Acceptance criteria
- [ ] Go program runs `whisper-cli` on a sample file with a chosen ggml model, parses output (JSON/SRT) and progress
- [ ] Cancellation (process kill) leaves no corrupt output; exit codes and stderr are captured
- [ ] Document where official Windows binaries/models come from, expected checksums, and how to pin model + version
- [ ] Measured speed (real-time factor) on the user's Windows machine is recorded for information only; no tuning
