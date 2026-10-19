// Package summaryexport renders the summary of a lesson for the clipboard and for a file the user saves (TASK-087):
// the title, the date and length of the lesson, the summary text, its key points and the homework found in the lesson.
// RenderLectureText renders the lecture text (TASK-090) the same way: headings, block times and paragraphs. It is
// pure: no I/O, no clock.
//
// The text comes from model output (hard rule 3) and is data only: plain text is written as it is, Markdown is
// escaped line by line (shared.EscapeMarkdown), so it can form neither HTML nor links nor other markup.
//
// Rules that apply: ARCH-10, ARCH-15, ARCH-17.
package summaryexport
