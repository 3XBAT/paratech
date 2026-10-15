// Package audiocapture captures system audio into chunk files and assembles the final track (PRD sections 6.2 and
// 6.3, ADR-0002 (audio capture)); Recorder satisfies the Recorder port of the recording session structurally.
//
// The capture runs in a child process of the same executable (the --recorder run mode, RunChild) for crash
// isolation. The parent writes one JSON start request to the child's stdin and stops it by closing stdin; the child
// writes JSON events to stdout (started, attempt, switch, chunk, stats as the heartbeat, failed, stopped). A child
// that crashes or hangs is restarted with a bounded backoff, in the mode the last one ended in, with the missed time
// as lead silence.
//
// Chunks are chunk-NNNNNN.wav files written through the Storage Manager (Create, then Rename from .partial when
// complete); a final name is always a complete WAV. Finalize converts the readable chunks, whatever their formats,
// into one 16 kHz mono 16-bit WAV track; the duration is counted in frames, never by the wall clock.
//
// The platform Source (WASAPI on Windows) is behind PlatformSource; this build has none yet, and Unavailable is the
// recorder of a build without capture.
package audiocapture
