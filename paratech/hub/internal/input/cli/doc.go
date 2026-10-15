// Package cli is the operator transport of the Hub: the "hub keys create|revoke|list" commands that issue, revoke
// and list per-tester API keys (PRD section 12, AC-25). Arguments are parsed by config (ARCH-52); this package only
// calls the keys use case and writes the result to its output (stdout). The plaintext key is written exactly once,
// by Create, and never to a logger.
package cli
