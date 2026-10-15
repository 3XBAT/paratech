// Package connections is the use case of the «Подключения» page (TASK-062, its own navigation page): the Hub address
// and the mail account (recipient, SMTP server, port, TLS mode, login) in the settings aggregate, the mail password in
// the credential store, and the "check mail" action, which opens a session without sending anything (PRD sections 10
// and 12, AC-19).
//
// Field rules stay in the settings aggregate; this package applies them, collects every invalid field and saves once.
// Secrets only go in: the use case can store, replace, remove and ask whether a secret exists, never read it back
// (hard rule 4). The Hub API key is the second slot (TASK-039): the hub store, entry api-key, checked by
// values.ParseHubAPIKey before it is stored, so a stored key can always be sent.
//
// The remaining quota (AC-12) is read from the Hub on RefreshQuota only and kept in memory for display
// (values.QuotaReading): a failed read keeps the last value as stale with its time, a missing or rejected key and a
// missing address drop it, and a change of the key or the address forgets it. The Hub stays the authority.
//
// Rules that apply: ARCH-20..27.
package connections
