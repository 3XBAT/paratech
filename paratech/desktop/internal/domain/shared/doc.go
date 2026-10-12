// Package shared is the shared kernel of the domain layer: typed identifiers that cross context
// boundaries (for example "type RecordingID string"), the artifact reference value object and base errors.
// Keep it minimal.
//
// Contexts never import each other (ARCH-11); they refer to foreign aggregates by these identifiers (ARCH-13)
// and use cases pass them through ports (ARCH-26).
//
// Rules that apply: ARCH-10 (std and itself only), ARCH-11 (it exists so contexts do not import each other).
package shared
