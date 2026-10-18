// Package transcription is the bounded context of local speech-to-text: installed STT models and the
// transcripts they produce (PRD sections 6.3 and 8, AC-06, AC-07, AC-08, AC-23).
// Aggregates: domain/transcription/sttmodel and domain/transcription/transcript. Domain service:
// domain/transcription/services/modelselection. Shared value objects (model identity, SHA-256, source URL, catalog
// manifest): domain/transcription/values.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	transcription/doc.go                  this file: purpose and glossary
//	transcription/<aggregate>/            one package per aggregate
//	transcription/values/                 value objects shared by several aggregates of the context
//	transcription/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - STT model: an installed speech-recognition model file with name, version, size, SHA-256 and capabilities.
//   - Catalog: the manifest of models the app can download; a model outside the catalog needs explicit user confirmation.
//   - Default model: the installed model used when a job does not pin another one.
//   - Pin: an STT job fixes the exact model and version it uses (reproducibility, safe deletion).
//   - Transcript: the text result of transcribing one recording, with the model name and version that produced it.
//   - STT engine: the local sidecar process (whisper-cli) that does the recognition; raw audio never leaves the machine.
//   - Speech component: the pinned files of the STT engine (the program and its libraries), shipped next to the
//     executable or installed from the official release archive into the data directory.
package transcription
