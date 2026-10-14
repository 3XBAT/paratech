// Package smtp is the EmailAdapter of the delivery flow (senddelivery.EmailAdapter; PRD sections 4.3 and 10): it sends
// one homework email with github.com/wneessen/go-mail.
//
// Security: TLS is mandatory (STARTTLS required, or implicit TLS); there is no plaintext fallback and no option to skip
// certificate verification (Config.RootCAs only replaces the trusted roots, for tests). The password is read by the
// adapter itself from the credential store (SecretReader, the wincred adapter in the app) at every send; it never
// passes through the core, SQLite or logs (hard rule 4, AC-19). The recipient and the subject come from the delivery
// aggregate, which refuses CR, LF and other control characters; go-mail encodes the headers.
//
// The attachment is read once, its SHA-256 and size are compared with the confirmed content of the delivery, and
// exactly those bytes are sent (delivery.ErrAttachmentChanged otherwise).
//
// Errors are classified by when they happen, so that a message that may have been accepted is never reported as
// "not sent" (hard rule 7, AC-15):
//
//	before MAIL FROM (connect, TLS, AUTH)      network: ErrTransient; 4xx: ErrTransient; 5xx or configuration: ErrRejected
//	MAIL FROM, RCPT TO, DATA command           4xx or connection lost: ErrTransient; 5xx: ErrRejected
//	while writing the message                  ErrOutcomeUnknown
//	reply after the terminating dot            250: sent; 4xx: ErrTransient; 5xx: ErrRejected; none (timeout, drop): ErrOutcomeUnknown
//
// A cancelled context closes the connection at once; the phase it interrupted decides the class as above. Error texts
// are fixed and contain at most the SMTP reply code, never the server's text, addresses or credentials.
//
// Reconcile cannot look a message up over SMTP and always returns delivery.ErrOutcomeUnknown (best effort; the user
// resolves an UNKNOWN delivery).
package smtp
