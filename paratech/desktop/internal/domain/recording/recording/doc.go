// Package recording is the aggregate of one recording of an online class (PRD sections 6.2 and 6.3): the lifecycle
// state machine as a table (transitions.go), the hard duration cap applied to the recording, the chunk directory and
// the final track, and the guard of deletion (ADR-0004(a)). Time is always a parameter.
//
// Artifact layout: the chunks live in the directory recordings/<id>, the final track is recordings/<id>.<ext>.
package recording
