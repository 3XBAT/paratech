// Package loadpolicy is the domain service that turns the recognition load mode into the policy of one speech
// recognition process (TASK-084, ADR-0016): threads, priority and the hard CPU cap. It is the one place of that rule;
// the whispercli adapter applies the result and the speed estimate scales by it. It is pure: the caller passes the
// logical CPU count.
//
// Rules that apply: ARCH-10..19.
package loadpolicy
