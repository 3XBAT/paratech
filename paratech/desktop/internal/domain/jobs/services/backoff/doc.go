// Package backoff is the domain service of retry policy for jobs (PRD section 7.3, AC-10): bounded exponential delays,
// a cap on attempts and the classification of handler errors. It is pure and deterministic; jitter comes in as a
// parameter (ARCH-15). WaitStatus and Rechecks decide when an LLM job waits for the network or the quota and when it
// is tried again (PRD sections 7.2 and 9.2).
package backoff
