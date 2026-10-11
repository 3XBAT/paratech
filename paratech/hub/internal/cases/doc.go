// Package cases is the application layer root: one package per use case (cases/<usecase>) with
// service.go, ports.go, types.go and errors.go (docs/architecture.md section 4).
//
// Rules that apply: ARCH-20 (imports only domain/**, std, internal/metrics; no other cases, adapters, input),
// ARCH-21 (ports are declared by the consumer), ARCH-22 (no business invariants here),
// ARCH-23 (one transaction = one use case call = one aggregate), ARCH-24 (time and randomness via ports),
// ARCH-25 (context.Context first), ARCH-26 (only domain types or primitives cross a port),
// ARCH-27 (side effects only after validation and policy; LLM output is untrusted).
package cases
