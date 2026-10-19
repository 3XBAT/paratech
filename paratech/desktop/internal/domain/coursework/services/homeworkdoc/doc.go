// Package homeworkdoc builds the document of a homework draft (TASK-094): the title page from the student profile and
// the lesson of the assignment, then the draft text as headings and paragraphs. It is pure: no I/O, no clock (the year
// is a parameter). An adapter renders the result (values.Document) to a file format.
//
// The draft text may be model output (hard rule 3): it is data only. Markdown headings ("#", "##", "###") become
// headings and blank lines separate paragraphs; no other markup is interpreted, and the renderer escapes every text.
//
// Rules that apply: ARCH-10, ARCH-15, ARCH-17.
package homeworkdoc
