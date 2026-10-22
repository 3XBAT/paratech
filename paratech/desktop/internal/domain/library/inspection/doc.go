// Package inspection is the aggregate of the reading of one material by the quarantined reader INSPECT_MATERIAL
// (ADR-0022, TASK-122): the request the Hub gets (Request), the strict check of the answer (Request.Parse) and the
// stored outcome per material (Inspection).
//
// The material text is untrusted data. The answer holds only a document kind, a subject ID from the candidates sent,
// a lesson date and three flags; anything else refuses the whole answer, so no text of the file becomes an
// instruction, an address or an action. The outcome is a suggestion only: it never files, links or sends anything.
//
// An Inspection is kept per material and text digest: a done one is the cache of that text; a refused one counts
// its attempts, and each new attempt has its own idempotency key (a refusal is charged and replayed by its key), up
// to MaxAttempts.
package inspection
