// Package retranscribe is the domain service that decides whether a recording may be transcribed again after its
// transcription was cancelled («Расшифровать снова», TASK-392) or failed (TASK-409) and which idempotency key the new
// STT job gets. It is
// the one place of that rule; the transcription use case reads the STT jobs and the transcript and acts on the
// decision. It is pure: the caller passes what it read.
//
// Rules that apply: ARCH-10..19.
package retranscribe
