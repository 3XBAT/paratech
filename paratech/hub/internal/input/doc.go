// Package input is the inbound layer root: one package per transport (input/connect for Connect-RPC handlers,
// input/cli for key-issuing commands), see docs/architecture.md section 6.
//
// Rules that apply: ARCH-40 (imports cases/*, domain/**, std, internal/metrics, third-party and shared/gen; never adapters or app),
// ARCH-41 (thin: parse, authenticate transport, call a use case, map the result), ARCH-42 (own narrow interfaces over use cases),
// ARCH-43 (transport-specific encoding lives here).
package input
