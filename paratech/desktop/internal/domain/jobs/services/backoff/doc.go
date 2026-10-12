// Package backoff is the domain service of retry policy for jobs (PRD section 7.3, AC-10): bounded exponential delays,
// a cap on attempts and the classification of handler errors. It is pure and deterministic; jitter comes in as a
// parameter (ARCH-15).
package backoff
