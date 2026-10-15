// Package connect is the Hub's HTTP transport: a chi router with the Connect-RPC handlers of shared/gen, the
// liveness probe /healthz and the readiness probe /readyz (docs/architecture.md section 6).
//
// The router is the public, API-key listener. Every RPC passes the API-key middleware (auth.go): a missing,
// malformed, unknown, revoked or wrong key gets one and the same unauthenticated error with ERROR_CODE_INVALID_API_KEY,
// and handlers find the caller with APIKeyFrom. The probes are not authenticated. Of QuotaService only GetRemaining
// is reachable here; Reserve, Settle and Release are Hub-internal (ADR-0005(f)) and answer 404 as if they did not exist.
package connect
