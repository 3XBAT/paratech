// Package soundcheck is the use case of "Check sound" on the diagnostics page (TASK-075, replacing the tone check of
// TASK-058): for a bounded time the recorder captures the source a recording would use (the app remembered in the
// settings or the whole computer) without recording, and the page polls the level it hears. The verdict follows the
// domain rule (values.LevelCheck.Verdict). While a recording runs the check is refused; one that starts stops it.
//
// The page has no event channel, so a check is an asynchronous session: Start launches it on a goroutine bounded by
// the service's own lifetime (not the request), Status reads it, Stop ends it early and Close ends it for good.
//
// Rules that apply: ARCH-20..27.
package soundcheck
