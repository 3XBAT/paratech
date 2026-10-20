// Package deletion is the domain service of deleting a recording (TASK-095, ADR-0018): what a recording holds that is
// deleted with it, and which of the homework assignments found in its transcripts may be deleted too. An assignment
// with a draft is deleted only when the user chose it explicitly; one with email history is never deleted, only
// detached from the recording, because its deliveries keep referencing it (the history of what was sent stays). The
// facts are primitives and shared ids, so the service knows no other context.
package deletion
