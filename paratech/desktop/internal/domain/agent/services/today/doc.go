// Package today holds the deterministic rules of the «Сегодня» screen (TASK-125, design §2.7): calendar days in the
// student's zone, «Горит на 7 дней», the card of a post after a lesson and the hint chips. No LLM is involved; the
// texts are impersonal templates (ADR-0021).
//
// Rules that apply: ARCH-17 (no state, no infrastructure). Times are read in the location of the given now.
package today
