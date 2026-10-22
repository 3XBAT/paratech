// Package actionpolicy is the action policy of the agent (ADR-0021, design §2.4): the class of every tool (R, U, C,
// UI; a tool outside the table is an error), when a write needs an approved plan first (NeedsPlan, threshold D8) and
// the typed provenance check of IDs (CheckProvenance). Replacing a ready result is class C (Classify).
//
// Rules that apply: ARCH-17 (no state, no infrastructure).
package actionpolicy
