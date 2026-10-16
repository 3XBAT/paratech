// Package soundcheck is the use case of "Check sound" on the diagnostics page (TASK-058, AC-3): a short tone is played
// on the default output device and captured by the recorder (process loopback INCLUDE of its own process and classic
// loopback, ADR-0002 Consequences); the answer for the user is the verdict of the domain for the source they record
// (values.SoundCheck.Verdict). While a recording runs the check is refused, its tone would disturb the lecture.
//
// Rules that apply: ARCH-20..27.
package soundcheck
