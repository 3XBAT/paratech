// Package importplan computes the preview of a schedule import (PRD sections 11.2 and 14 case 9, AC-16): it validates
// the parsed rows and diffs them against the live lessons. It is pure: no I/O, no clock, no ids.
//
// Identity. A row carries a Key chosen by its parser; the stored source uid is "<source>:<key>", for example
// "csv:id:42". Rows are matched to live lessons by that uid.
//
// Scope of removal. An import of source S is the full schedule of S: a live lesson whose uid starts with "S:" and that
// the file no longer contains is removed (soft delete). Manual lessons (no uid) and lessons of other sources are never
// removed or changed by it. A parser that reads only a time range of the file (Parsed.Window, the import dates of
// ICS) limits this to lessons starting in the window: the source's lessons outside it are kept, so a later import
// with a moved window does not remove past lessons. Rows must start inside the window.
//
// Conflicts are warnings, the user may still commit (PRD section 14 case 9: preview and explicit commit). Two kinds:
// two rows of the file overlap in time, or a row overlaps a live lesson the import keeps (manual or another source).
// Touching ranges do not overlap.
//
// Errors. Any row error (from the parser or from validation here) makes the whole plan uncommittable: the plan then
// holds only the errors, never a partial diff.
package importplan
