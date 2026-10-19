// Package speedestimate is the domain service that estimates how long speech recognition takes on this computer
// (TASK-084, ADR-0016): the processing time of 1 h of audio for a model in a load mode, from a rough table scaled by
// the run policy or, once runs were measured here, from the measured speed; and the time left of a queued or running
// recognition. It is pure: the caller passes the logical CPU count, the measured speed and the elapsed time.
//
// Rules that apply: ARCH-10..19.
package speedestimate
