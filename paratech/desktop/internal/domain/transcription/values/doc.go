// Package values holds value objects shared by several parts of the transcription context (PRD sections 8 and 8.1):
// the identity of a model (name, version, catalog id), its SHA-256, quantization and capabilities, the source URL,
// the machine profile used for recommendations, the catalog manifest with the model family and its load on the
// computer (LoadOf), the payload of an STT job (STTRequest) and the pinned speech recognition component
// (SpeechComponent, TASK-072).
//
// The catalog manifest is a trust boundary (docs/process/security-review.md): ParseCatalog accepts only a bounded,
// strictly shaped JSON document with https source URLs without credentials, lowercase-hex SHA-256 values and
// no duplicate entries. URLs are data here; nothing in the domain fetches them.
// The speech component manifest is parsed as strictly; its file names are plain lowercase names, so an archive entry
// can only be matched to them, never turned into a path.
//
// Rules that apply: ARCH-10..19.
package values
