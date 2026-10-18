// Package recording is the bounded context of capturing the audio of an online class (PRD sections 6.1 to 6.3).
// Aggregates: domain/recording/recording, domain/recording/prompt (the offer to record). Domain services:
// domain/recording/services/limits (hard cap, one recording at a time). Values: domain/recording/values (the
// recording source and the source picker).
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	recording/doc.go                  this file: purpose and glossary
//	recording/<aggregate>/            one package per aggregate
//	recording/values/                 value objects shared by several aggregates of the context
//	recording/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Recording: one capture of system audio (WASAPI loopback) of an online class, stored as a single track.
//   - Status: STARTING, RECORDING, STOPPING, FINALIZING, COMPLETED, FAILED, RECOVERY_REQUIRED (PRD section 6.2).
//   - Chunk: a piece of audio written to disk while recording, so a crash loses at most the latest chunk.
//   - Hard cap: the maximum recording duration (about 3 hours); at the cap the recording is finalized.
//   - Heartbeat: a periodic liveness mark of an active recording; a stale heartbeat after restart means a crash.
//   - Recovery: assembling the readable chunks of an interrupted recording into the final file.
//   - Prompt: one offer to record, shown in the non-modal popup: for a lesson that is starting (from the schedule) or
//     ad hoc (asked for by the user). States OFFERED, STARTING, STARTED, SKIPPED, EXPIRED (no snooze).
//   - Expiry: a lesson prompt that nobody acted on ends when its lesson ends.
//   - Source: what a recording captures: one app (by executable name, with its process tree) or the whole computer
//     except this app (the fallback). The picker lists the open apps; the choice is remembered per app.
package recording
