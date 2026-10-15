// Package audiocapture is the TASK-015 spike: how to capture system audio on Windows from Go
// (PRD sections 4.3, 6.2 and 6.3; AC-03, AC-05, AC-24).
//
// It is throwaway prototype code. Nothing outside spikes/ may import it (tests/arch_test.go enforces this);
// the product recorder is TASK-017 and only reuses the findings recorded in the audio-capture ADR.
//
// Candidates:
//   - wasapi/: pure Go COM bindings (x/sys, syscall, unsafe; no CGO). Process loopback INCLUDE of the app the
//     user picked is the primary mode; "whole system" is a fallback (process loopback EXCLUDE of our own PID, or
//     classic loopback on the default render endpoint with a silent keep-alive render stream).
//   - cmd/capturespike -child: the same binary re-executed with --recorder as a crash-isolated recorder process.
//   - malgo/: a separate Go module using github.com/gen2brain/malgo (miniaudio, needs CGO). It exists only to be
//     compared and is rejected for the product because of CLAUDE.md hard rule 1 (no CGO in desktop).
//
// wavchunk/ writes closed WAV chunks (each has a final header, so a kill loses at most the current chunk) and
// validates them.
//
// Run on Windows (from paratech/):
//
//	go build -o capturespike.exe ./desktop/spikes/audio-capture/cmd/capturespike
//	capturespike.exe -mode auto -exe chrome.exe -out C:\tmp\capture -chunk 30s -duration 10m
//	capturespike.exe -mode system -out C:\tmp\capture -duration 10s -child
package audiocapture
