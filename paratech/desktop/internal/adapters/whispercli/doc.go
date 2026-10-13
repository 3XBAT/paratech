// Package whispercli is the local speech-to-text adapter: it satisfies the STTEngine port of cases/transcribe
// structurally by running the whisper.cpp command-line tool (whisper-cli) as a separate, CPU-only process for each
// transcription (PRD sections 4.3 and 8, AC-06; hard rule 2: audio never leaves the machine).
//
// One call is one process: no shell, no server, no ports. Arguments are built only from validated values: the
// absolute model and audio paths given by the app's root-confined resolver, an output prefix in a private run
// directory below the data root, the configured thread count and a language from the whisper.cpp allowlist. The
// binary path comes from configuration only, never from job data. The process gets an explicit, minimal
// environment (nothing from the parent), a fixed working directory (its run directory) and no stdin; stdout,
// which carries the recognised text, is discarded; stderr is kept in a bounded tail for error messages (with the
// data root redacted) and parsed for progress.
//
// The result is the JSON file of the CLI (-oj). It is read with a size limit, parsed strictly (required fields,
// no trailing data, validated timestamps) and stored as a normalised transcript document through the artifact
// store (an atomic write), only after a successful run. Cancelling the context kills the process (on Windows the
// whole process tree, through a job object) and the run directory is removed, so no partial output remains.
//
// Verified CLI flags, exit codes and output formats: docs/refs/whisper.md. Rules that apply: ARCH-30..33.
package whispercli
