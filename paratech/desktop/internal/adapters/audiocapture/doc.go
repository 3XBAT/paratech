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
// The platform Source is PlatformSource: on 64-bit Windows pure-Go WASAPI (COM through vtables, no CGO) with process
// loopback of the target and classic loopback of the default output device as the one-way fallback (after an
// activation failure, a failed stream, or a healthy-but-mute stream: mute.go); elsewhere none (Supported is false),
// and Unavailable is the recorder of such a build. Recorder.Diagnostics reports the capture (TASK-058) as the domain
// values.CaptureDiagnostics: modes, attempts with HRESULTs, switches with causes, Windows version, output device;
// RecentCaptures lists them for the support bundle.
//
// Recorder.CheckLevels is "Check sound" (levelcheck.go, TASK-075): a child with a level check request captures the
// user's source as a recording would, for a bounded time, and reports the peak of each 50 ms window; nothing is played
// or written.
package audiocapture
