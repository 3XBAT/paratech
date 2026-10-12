// Package transcribe is the use case of turning a recording into a transcript locally: the handler of the STT job,
// which runs the speech-to-text sidecar with the pinned model, reports progress, honours cancellation and the power
// policy, and stores the result (PRD sections 4.3, 7.4 and 8, AC-06, AC-08, AC-21).
//
// TASK-003 contains only the ports (ports.go). The Service arrives with TASK-024 (engine adapter contract) and
// TASK-029 (the STT job handler).
//
// Rules that apply: ARCH-20..27. Raw audio never leaves the machine: no port of this package talks to the cloud.
package transcribe
