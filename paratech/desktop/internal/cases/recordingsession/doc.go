// Package recordingsession is the use case of recording a class: start and stop from the popup or the UI,
// chunked capture of system audio, the duration cap, the heartbeat and recovery of interrupted recordings
// (PRD sections 6.1 to 6.3 and 14 case 1 and 2, AC-01 to AC-05, AC-24).
//
// The Service (service.go, TASK-013) starts and stops recordings (AC-03), follows the capture through the Recorder,
// finalizes at the hard cap (AC-04) or when the capture ends by itself, and deletes finished recordings with their
// transcripts (ADR-0004(a)). Not here yet: the heartbeat writes and the startup recovery (TASK-014) and the
// "lesson started" popup scenario (TASK-022).
//
// Rules that apply: ARCH-20..27. The state machine of a recording lives in the recording aggregate, not here.
package recordingsession
