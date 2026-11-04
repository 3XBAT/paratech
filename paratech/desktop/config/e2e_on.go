//go:build e2e

package config

import "flag"

// registerE2EFlags adds the flags of the E2E mode (ADR-0035 §3); only a build with tag e2e has them.
func registerE2EFlags(fs *flag.FlagSet, e *E2E) {
	fs.StringVar(&e.Dir, "e2e-dir", "", "e2e build: absolute control directory of an end-to-end run; turns the E2E mode on")
	fs.IntVar(&e.CDPPort, "e2e-cdp-port", 0, "e2e build: WebView2 remote debugging port, 1024..65535 (needs -e2e-dir)")
	fs.StringVar(&e.Window, "e2e-window", "", "e2e build: main window size WxH, each 640..3840 (needs -e2e-dir)")
}
