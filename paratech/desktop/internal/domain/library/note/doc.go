// Package note is the aggregate of the student's own notes of one lecture (ADR-0027): the subject, the anchor (a
// recording, a schedule lesson, a day or no date) with its snapshot, a display title, the ordered parts (library
// materials, by id) and the status active or removed.
//
// A note holds only the student's text: nothing generated is stored in it. The anchor names a recording or a lesson
// by id only, with no foreign key; when either is gone the anchor falls back to the day of its snapshot, so deleting a
// recording or re-importing the schedule never deletes or orphans the notes.
package note
