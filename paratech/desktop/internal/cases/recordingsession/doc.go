// Package recordingsession is the use case of recording a class: start and stop from the popup or the UI,
// chunked capture of system audio, the duration cap, the heartbeat and recovery of interrupted recordings
// (PRD sections 6.1 to 6.3 and 14 case 1 and 2, AC-01 to AC-05, AC-24).
//
// The Service (service.go, TASK-013) starts and stops recordings (AC-03), follows the capture through the Recorder,
// finalizes at the hard cap (AC-04) or when the capture ends by itself, writes the heartbeat of the running capture,
// and deletes finished recordings with their transcripts (ADR-0004(a)). Recover (recover.go, TASK-014) finishes the
// recordings an earlier run left unfinished (AC-05); RecoveryMarked tells when its marking pass is over, so the
// recording prompt (cases/recordingprompt) starts nothing before it. Not here yet: the UI action that offers a recording
// kept for recovery to the user.
//
// While a recording of one app runs, the session also watches for the silent-source advice (advice.go, TASK-082): the
// capture hears nothing while another app plays; SwitchSource moves the same recording to another source and
// DismissAdvice hides the advice. A track that is silent all along is marked so and not transcribed.
//
// Rules that apply: ARCH-20..27. The state machine of a recording lives in the recording aggregate, not here.
package recordingsession
