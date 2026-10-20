// Package summary is the aggregate of the summary of one lesson transcript (PRD sections 9 and 10, AC-13).
//
// There is one summary per transcript: its id is the transcript id, so reprocessing the transcript replaces the summary
// instead of adding a second one. Since TASK-101 a summary is made of headed sections (heading, text of 1-3
// paragraphs, optional points); a summary made before is a plain text, read as one section without heading. The text
// comes from model output and is data only (hard rule 3). The summary stays
// when its transcript is deleted (the reference becomes empty), like the assignments of the lesson.
package summary
