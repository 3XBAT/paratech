// Package gateway is the LLM gateway use case (PRD sections 4.2, 9.2, 13.2; gateway.proto; ADR-0009): rate limit the
// tester, validate the completion request, reserve the most it may cost, call the provider chosen server-side, then
// settle the actual usage and store the answer, or release. A retry with the same idempotency key replays the stored
// answer. The key rules live in the account aggregate; every ledger step is one transaction (ARCH-23).
package gateway
