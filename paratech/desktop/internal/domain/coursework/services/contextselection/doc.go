// Package contextselection picks the minimal local context an LLM operation needs (PRD section 9.1: raw local data is
// not uploaded merely because it exists). It is pure: no I/O, no clock, no ids.
//
// The caller gathers what it has into a Material; Select keeps only the fields the operation uses and drops the rest:
//
//   - summary, extract_homework_deadlines and lecture_text: the transcript and the lesson context (TASK-093): title,
//     subject, kind, teacher, start (the anchor for relative dates) and the start and kind of the next lesson of
//     the same subject (the anchor for "by the next class"). lecture_text is sent in parts with segment start times
//     (domain/coursework/services/lectureparts, TASK-090);
//   - homework_draft: the lesson title, subject and teacher, the assignment title and description, the lesson summary,
//     the previous draft and the student's refinement request. The transcript is not sent: the summary stands for it.
//
// Segment timestamps (except in the parts of lecture_text), the assignment deadline and anything else the caller
// holds are never selected. Every selected text is user data: domain/coursework/services/prompts puts it into the
// untrusted part of the request only.
package contextselection
