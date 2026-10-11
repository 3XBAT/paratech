// Package shared is the shared kernel of the domain layer: typed identifiers that cross context
// boundaries (for example "type RecordingID string") and base errors. Keep it minimal.
//
// Rules that apply: ARCH-10 (std and itself only), ARCH-11 (it exists so contexts do not import each other).
package shared
