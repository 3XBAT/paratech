// Package delivery is the aggregate of sending an artifact to a recipient by email (PRD sections 10 and 14, AC-14,
// AC-15, hard rule 7).
//
// A delivery holds the recipient, the subject, the attachment with the content the user confirmed (SHA-256 and size),
// the Message-ID generated before the first attempt, the idempotency key and the confirmation time; New refuses a
// delivery without a confirmation (AC-14).
//
// Status machine (transitions.go, ADR-0004(c)):
//
//	PENDING -> SENDING | FAILED
//	SENDING -> SENT | PENDING | FAILED | UNKNOWN
//	UNKNOWN -> SENT | PENDING
//
// SENT and FAILED are terminal. SENDING is saved before the message is handed to the provider; the result of the
// attempt is classified by OutcomeOf, and anything that does not prove "not accepted" becomes UNKNOWN. A SENDING
// delivery left by a crash becomes UNKNOWN (Interrupted). UNKNOWN is never sent again by itself: reconciliation or the
// user resolves it with MarkSent ("mark as sent") or SendAgain ("send again").
//
// Each send round has one job (JobKey); only SendAgain starts a new round. On the last attempt of the job a retry
// outcome ends FAILED (RecordFinalResult), so a delivery never stays PENDING without a job to send it.
package delivery
