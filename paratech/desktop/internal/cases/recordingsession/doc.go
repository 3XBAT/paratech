// Package recordingsession is the use case of recording a class: start and stop from the popup or the UI,
// chunked capture of system audio, the duration cap, the heartbeat and recovery of interrupted recordings
// (PRD sections 6.1 to 6.3 and 14 case 1 and 2, AC-01 to AC-05, AC-24).
//
// TASK-003 contains only the ports (ports.go). The Service with the scenarios arrives with TASK-013
// (state machine, cap), TASK-014 (watchdog, recovery) and TASK-022 (the "lesson started" popup scenario).
//
// Rules that apply: ARCH-20..27. The state machine of a recording lives in the recording aggregate, not here.
package recordingsession
