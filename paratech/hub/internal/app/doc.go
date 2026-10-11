// Package app is the composition root and lifecycle owner (docs/architecture.md section 7).
//
// Rules that apply: ARCH-50 (only app knows all layers and wires adapters, use cases and input; New, Run, Close),
// ARCH-53 (no mutable globals, no init() side effects, no singletons).
package app
