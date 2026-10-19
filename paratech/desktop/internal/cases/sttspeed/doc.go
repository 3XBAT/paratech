// Package sttspeed is the use case of the recognition time estimates (TASK-084, ADR-0016): for every catalog and
// installed model the processing time of 1 h of audio in each load mode on this computer, from the table of
// speedestimate or the speed measured here, and the processing time of one recording for the time left of a queued or
// running transcription. It only reads; the transcribe use case records the measured speed.
//
// Rules that apply: ARCH-20..27.
package sttspeed
