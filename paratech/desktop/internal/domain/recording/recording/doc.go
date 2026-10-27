// Package recording is the aggregate of one recording of an online class (PRD sections 6.2 and 6.3): the lifecycle
// state machine as a table (transitions.go), the hard duration cap applied to the recording, the chunk directory and
// the final track, the guard of deletion (ADR-0004(a)) and the decisions of the recovery after a restart
// (MarkInterrupted, CompleteRecovery, FailRecovery, DeferRecovery; AC-05). Time is always a parameter.
//
// A recording is captured or imported (ADR-0027): NewImported makes an audio file recorded elsewhere a COMPLETED
// recording of its lecture, its format told from the content by DetectAudio.
//
// Artifact layout: the chunks live in the directory recordings/<id>, the final track is recordings/<id>.<ext>.
package recording
