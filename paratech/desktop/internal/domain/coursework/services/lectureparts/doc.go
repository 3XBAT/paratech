// Package lectureparts splits a transcript into the parts of the lecture text and merges the formatted parts back
// (TASK-090, ADR-0017). It is pure: no I/O, no clock.
//
// Plan cuts the segments into parts by time and size, so that the formatted answer of one part fits the output limit
// of the model. Each part after the first carries the end of the part before as read-only context (continuity only:
// the model is told not to output it), so the parts need no overlap in what is formatted and the merge needs no LLM.
// Part.Starts maps the segment indexes the model names to start times, never trusting them beyond the part.
// Merge concatenates the blocks of the parts in order, drops a paragraph the model repeated across a cut and joins a
// block that only continues the heading before it.
//
// Rules that apply: ARCH-10, ARCH-15, ARCH-17.
package lectureparts
