// Package lecturetext is the aggregate of the lecture text of one transcript (TASK-090): the full readable text of the
// lesson, formatted by the LLM part by part, with short headings, the start time of each block and its paragraphs.
//
// There is one lecture text per transcript: its id is the transcript id. A run formats the parts of the transcript in
// order (AddPart); the finished parts are kept, so a run that stops continues from the next part. Once every part is
// there, the merged blocks replace the text shown (Publish): a text made before stays until a new run completes. The
// text is model output and data only (hard rule 3).
package lecturetext
