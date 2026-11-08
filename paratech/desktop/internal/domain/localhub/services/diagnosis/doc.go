// Package diagnosis is the rule of the local Hub context (ADR-0036 section 4): Diagnose turns the probe facts into
// the status, reason and next step the owner sees, and says whether the Hub may be started. Pure: no I/O, no time.
//
// Rules that apply: ARCH-10..19.
package diagnosis
