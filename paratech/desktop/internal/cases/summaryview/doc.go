// Package summaryview is the use case of the summary viewer (TASK-087): the summary of a recording with its key
// points, the homework found in the lesson, where the summary and the homework search stand, the text for the
// clipboard and the export to a file the user chose. The lecture text (TASK-090) is shown, copied and saved the same
// way. It reads local post-processing results only and works without the network.
//
// The summary, the lecture text and the assignment texts are model output (hard rule 3): they are returned as data,
// never logged.
//
// Rules that apply: ARCH-20..27.
package summaryview
