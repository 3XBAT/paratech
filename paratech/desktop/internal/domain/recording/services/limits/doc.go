// Package limits is the domain service of recording limits (PRD sections 6.2 and 6.3, AC-04): the configurable hard
// cap of one recording, about 3 hours by default, and the rule that only one recording is captured at a time (the
// app records one system audio track). It is pure; the recording aggregate stores the cap it was created with.
package limits
