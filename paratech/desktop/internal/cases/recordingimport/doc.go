// Package recordingimport is the use case of importing an old recording of a lecture made elsewhere (TASK-161,
// ADR-0027 «Old recordings made elsewhere»): an audio file the student picked becomes a COMPLETED recording of that
// lecture, and the usual local transcription and everything after it run unchanged.
//
// An import runs in this order:
//  1. The file is opened read-only; its size is checked (recording.CheckImportSize) and its format told from its first
//     bytes (recording.DetectAudio), never from its name.
//  2. It is copied into recordings/<id>.<ext> through the Storage Manager (atomic write) while its SHA-256 is computed.
//  3. One transaction finds a recording with the same digest (the same bytes are the same recording: the copy is
//     deleted and that one is returned) or stores the new one (recording.NewImported).
//  4. The completion listener enqueues the transcription job (power-aware, model pinned), as after a capture.
//
// Hard rule 2: the audio never leaves the machine. No port of this package reaches the network; the original file is
// only read, and deleting the recording later (ADR-0018) removes the copy only. A crash between steps 2 and 3 leaves
// a copy without a row.
//
// Rules that apply: ARCH-20..27.
package recordingimport
