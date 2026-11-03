// Package recordingimport is the use case of importing an old recording of a lecture made elsewhere (TASK-161,
// ADR-0027 «Old recordings made elsewhere»): an audio file the student picked becomes a COMPLETED recording of that
// lecture, and the usual local transcription and everything after it run unchanged.
//
// An import runs in this order:
//  1. The file is opened read-only; its size is checked (recording.CheckImportSize) and its format and duration told
//     from its first and last bytes (recording.DetectAudio), never from its name.
//  2. The original is read through once for its SHA-256 and, for an MP3, its frames (recording.MP3FrameCounter); a
//     file too long or of unknown duration is refused here. A recording with the same digest is returned as it is
//     (the same bytes are the same recording), with no copy written.
//  3. It is copied into recordings/imp-<id>.<ext> through the Storage Manager (atomic write), hashed again on the
//     way; a copy whose digest differs from step 2 (the file changed meanwhile) is deleted and refused (ErrChanged).
//  4. One transaction stores the new recording (recording.NewImported), or returns one of the same digest stored
//     meanwhile and deletes the copy.
//  5. The completion listener enqueues the transcription job (power-aware, model pinned), as after a capture.
//
// Hard rule 2: the audio never leaves the machine. No port of this package reaches the network; the original file is
// only read, and deleting the recording later (ADR-0018) removes the copy only.
//
// A crash between steps 3 and 4 leaves a copy without a row. SweepOrphans, run at start-up, deletes such copies: the
// id of an imported recording starts with "imp-" (generated ids never hold a hyphen), so only a file named
// recordings/imp-<id>.<wav|flac|mp3|ogg>, older than OrphanAge and without a recording of its id is deleted; the
// chunks and tracks of a capture are never touched.
//
// Rules that apply: ARCH-20..27.
package recordingimport
