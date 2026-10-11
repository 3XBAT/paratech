// Package recording is the bounded context of capturing the audio of an online class (PRD sections 6.2 and 6.3).
// Aggregates: domain/recording/recording. Domain services (domain/recording/services/limits) are added by later tasks.
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
package recording
