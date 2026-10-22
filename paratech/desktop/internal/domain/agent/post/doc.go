// Package post is the aggregate of a post after a lesson (ADR-0021 «Posts after a lesson», design §2.7, TASK-125):
// what the post-processing of one recording produced, shown on «Сегодня» and in the thread of the lesson's subject.
// No LLM writes it: the post records only that the summary is ready and that the homework was looked for.
//
// The recording is the identity and the dedup key: one post per recording, so a repeated completion event changes
// nothing. An event of another transcript of the recording (it was recognized again) switches the post to that
// transcript and drops what the old one had produced.
package post
