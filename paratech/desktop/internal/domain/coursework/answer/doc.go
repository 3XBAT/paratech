// Package answer is the aggregate of an answer to a student's question from their own lessons (TASK-118, ADR-0022):
// the question, the fragments of lessons sent to the quarantined reader ANSWER, its claims with the citations that
// passed the check of coursework/services/citation, the parts it could not answer, and the places in the lessons the
// citations open. An answer is written once and never changes. A place is in a recording or, since TASK-153, in the
// student's own note (SectionOwnNote, ADR-0027), named by OwnNoteLabel.
//
// The reader's answer is untrusted data. It is parsed strictly (Request.Parse) with the bounds the Hub checks
// (hub domain llm/services/answer); a citation counts only if its quote really is in the fragment it names. A claim
// left without such a citation is kept as unsupported and is never shown as «из ваших лекций».
package answer
