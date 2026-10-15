// Package connections is the use case of the "Connections" section of the settings page (TASK-062): the Hub address
// and the mail account (recipient, SMTP server, port, TLS mode, login) in the settings aggregate, the mail password in
// the credential store, and the "check mail" action, which opens a session without sending anything (PRD sections 10
// and 12, AC-19).
//
// Field rules stay in the settings aggregate; this package applies them, collects every invalid field and saves once.
// Secrets only go in: the use case can store, replace, remove and ask whether a secret exists, never read it back
// (hard rule 4). The Hub API key will get its own slot here (TASK-039) with the same SecretWriter and SecretStatus.
//
// Rules that apply: ARCH-20..27.
package connections
