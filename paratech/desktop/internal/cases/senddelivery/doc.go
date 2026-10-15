// Package senddelivery is the use case of emailing a finished homework artifact after explicit user confirmation:
// creating the delivery, sending it, storing the result and reconciling an unknown outcome instead of retrying
// blindly (PRD sections 10 and 14 case 7, AC-14, AC-15, hard rule 7).
//
// Flow: SendHomework takes a proposal.EmailCommand (only a CONFIRMED proposal has one), checks the attachment against
// the confirmed digest, stores a PENDING delivery keyed by the command's idempotency key and enqueues its job. The job
// (Handle) saves SENDING, calls the EmailAdapter outside any transaction and saves SENT, PENDING (transient), FAILED
// or UNKNOWN. UNKNOWN is never sent again on its own: the job reconciler and Recover handle interrupted attempts, and
// the user resolves an UNKNOWN delivery with MarkSent or SendAgain (or ReconcileDelivery where the provider can tell).
// A delivery has one job per send round (only SendAgain starts a new one), and the last attempt of that job ends a
// delivery that was not handed over as FAILED, so retries stay bounded (PRD section 7.3).
//
// Startup order in the app: jobrunner.Recover, Recover, Resume, then the runner's Start.
//
// The SMTP password is written to the credential store through CredentialStore and read only by the smtp adapter.
//
// Rules that apply: ARCH-20..27. The email adapter is reached only after validation and user confirmation
// (hard rule 3, ARCH-27); the status machine lives in the delivery aggregate.
package senddelivery
