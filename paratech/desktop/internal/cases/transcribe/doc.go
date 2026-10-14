// Package transcribe is the use case of turning a recording into a transcript locally: the handler of the STT job
// (JobType), which runs the speech-to-text sidecar with the pinned model, reports progress, honours cancellation and
// the power policy through the job runner, and stores the result (PRD sections 4.3, 7.4 and 8, AC-06, AC-08, AC-21).
//
// The app registers Service with the job runner as a Heavy type with the requeue recovery policy: the job has no
// external side effect and every attempt is idempotent (see Service). It publishes transcript.Created through
// EventPublisher; notifications and post-processing are the app's, not this package's.
//
// Rules that apply: ARCH-20..27. Raw audio never leaves the machine: no port of this package talks to the network,
// and tests/stt_network_test.go checks that no package on the STT path imports a network package.
package transcribe
