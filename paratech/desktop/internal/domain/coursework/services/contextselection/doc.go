// Package contextselection picks the minimal local context an LLM operation needs (PRD section 9.1: raw local data is
// not uploaded merely because it exists). It is pure: no I/O, no clock, no ids.
//
// The caller gathers what it has into a Material; Select keeps only the fields the operation uses and drops the rest:
//
//   - summary: the lesson title and the transcript text;
//   - extract_homework_deadlines: the lesson title, the lesson start (the anchor for relative dates) and the transcript;
//   - homework_draft: the lesson title, subject and teacher, the assignment title and description, the lesson summary,
//     the previous draft and the student's refinement request. The transcript is not sent: the summary stands for it;
//   - lecture_text: the lesson title, its subject and kind and the transcript. The kind only selects trusted wording.
//     The transcript is sent in parts with segment start times (domain/coursework/services/lectureparts, TASK-090).
//
// Segment timestamps (except in the parts of lecture_text), the assignment deadline and anything else the caller
// holds are never selected. Every selected
// text is user data: domain/coursework/services/prompts puts it into the untrusted part of the request only.
package contextselection
