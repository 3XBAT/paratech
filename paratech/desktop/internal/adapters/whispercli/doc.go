// Package whispercli is the local speech-to-text adapter: it satisfies the STTEngine port of cases/transcribe
// structurally by running the whisper.cpp command-line tool (whisper-cli) as a separate, CPU-only process for each
// transcription (PRD sections 4.3 and 8, AC-06; hard rule 2: audio never leaves the machine).
//
// One call is one process: no shell, no server, no ports. Arguments are built only from validated values: the
// absolute model and audio paths given by the app's root-confined resolver, an output prefix in a private run
// directory below the data root, the configured thread count and a language from the whisper.cpp allowlist. The
// binary path comes from configuration only, never from job data. The process gets an explicit, minimal
// environment (nothing from the parent), a fixed working directory (its run directory) and no stdin; stdout,
// which carries the recognised text, is discarded; stderr is kept in a bounded tail for error messages and parsed
// for progress. The tail is decoded from the CLI's encoding (the ANSI code page on Windows) and the data root and
// binary directory are redacted in both forms.
//
// The progress callback runs on a goroutine of the adapter, fed with the latest value, so a slow callback never
// blocks the stderr pipe; it must be safe to call from another goroutine. Calls never overlap and end before
// Transcribe returns.
//
// The result is the JSON file of the CLI (-oj). It is read with a size limit, parsed strictly (required fields,
// no trailing data, timestamps within 0..24 h; an odd segment order is clamped, not rejected) and stored as a
// normalised transcript document through the artifact store (an atomic write), only after a successful run.
// Cancelling the context kills the process (on Windows the whole process tree, through a job object) and the run
// directory is removed, so no partial output remains.
//
// Open points for the wiring and later tasks:
//   - The Paths resolver is filestore.Store.Path, which refuses links in every path component; this adapter Lstats
//     the last element again.
//   - The stored transcript document format is owned by the domain (transcript.Document); the adapter only fills it.
//   - On Windows a data root with characters outside the ANSI code page is rejected (ErrConfig, or ErrInvalidInput
//     per file). Whether the app uses an ASCII-only data root, or configures one, is a product decision for TASK-025.
//   - The run directory mode 0700 has no effect on Windows; there the data root's ACL protects the run directories.
//
// Verified CLI flags, exit codes and output formats: docs/refs/whisper.md. Rules that apply: ARCH-30..33.
package whispercli
