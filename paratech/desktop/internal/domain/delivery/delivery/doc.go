// Package delivery is the aggregate of one attempt to send an artifact to a recipient by email
// (PRD sections 10 and 14, AC-14, AC-15, hard rule 7).
//
// TASK-003 skeleton: only the identity, the status enumeration and the outcome errors of the email port
// exist so that use case ports can name the types. The status machine (including UNKNOWN and
// reconciliation), recipient, subject, artifact, Message-ID, the idempotency key and the New/Restore
// factories arrive with TASK-044.
//
// The PRD names only the UNKNOWN outcome (sections 10, 14 and 16); the other statuses are derived from the
// flow of section 10 and need confirmation, see Status.
package delivery
