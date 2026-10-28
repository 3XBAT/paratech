// Package assignment is the aggregate of one homework assignment (PRD sections 10 and 5.3, AC-13, AC-20).
//
// An assignment has a title, a description and an optional deadline (Details). The deadline is part of the
// assignment: "the deadlines" of a lesson are the due times of its assignments. The local row is the system of
// record; a calendar only shows it (AC-20).
//
// Assignments are created from the extraction answer of the LLM (TASK-042): each item of the answer is identified by
// its Origin, the transcript and its position in the answer, so that reprocessing the same transcript updates the
// assignment it made before instead of adding a second one. The values come from model output and are data only
// (hard rule 3): they never trigger a side effect.
//
// Matching on reprocessing is by position only, not by content: item i of the new answer updates the assignment of
// item i of the earlier one. A user-edited or done assignment keeps its slot, so a different new item at that position
// is not stored. Assignments at positions the new answer no longer has are withdrawn unless the user edited them or
// marked them done (Superseded) or a delivery still references them.
//
// The subject and teacher of the lesson come from the recording's lesson snapshot (outside this context). The
// extraction may add hints found in the transcript (model output, SetHints) and the user may correct both (Correct);
// values.NewLessonLabel decides what is shown. Hints are replaced by every extraction, even of a user-edited
// assignment: they are not user data.
//
// Invariant: once the user edited an assignment (Edit), reprocessing never changes it again (Reextract keeps it).
// While it is marked done (MarkDone), reprocessing neither changes nor withdraws it.
package assignment
