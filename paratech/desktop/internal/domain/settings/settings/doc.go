// Package settings is the aggregate of the application settings (PRD sections 6.1, 7.4 and 15,
// AC-02, AC-07, AC-22).
//
// The aggregate is a singleton. New returns the defaults of a fresh installation (recording prompts on, process
// only on AC power on, notifications on, no default STT model) with version 0; a repository returns them from Load
// while nothing is stored, and the first Save stores them (ADR-0004(d)). Restore rebuilds a stored row. Values change
// only through the Set/Select/Clear methods, which take the change time from the caller (ARCH-15).
//
// It also holds the non-secret mail and Hub setup (homework recipient, SMTP host, port, security mode and login, Hub
// URL; PRD sections 10 and 12), empty until the user sets them. The SMTP password and the Hub API key are never here:
// they live in the credential store (hard rule 4). Mail is configured when a server, a recipient and a login that is
// a plain address are set; that login is also the sender address.
//
// The zero value of Settings has every switch off, which is NOT the default: always use New or Restore.
package settings
