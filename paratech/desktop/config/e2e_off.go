//go:build !e2e

package config

import "flag"

// registerE2EFlags adds nothing: without tag e2e the E2E mode has no flags and Config.E2E stays zero (ADR-0035 §3).
func registerE2EFlags(*flag.FlagSet, *E2E) {}
