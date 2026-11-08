package backoff

import "time"

// InteractiveRetries is the most automatic retries of a call made for the student now (ADR-0026 addendum 2026-11-08).
const InteractiveRetries = 3

// Interactive is the wait before automatic retry attempt of a Hub call the student waits for (a planner step, an
// answer asked now): attempt counts the retries, 1 for the first, so the panel's «Повторная попытка N из 3» shows the
// same N. It is 5 s, 10 s, 20 s; ok is false for an attempt outside 1..InteractiveRetries. No jitter: one student, one
// call at a time.
func Interactive(attempt int) (wait time.Duration, ok bool) {
	if attempt < 1 || attempt > InteractiveRetries {
		return 0, false
	}
	return 5 * time.Second << (attempt - 1), true
}
