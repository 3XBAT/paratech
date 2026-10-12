//go:build !windows

package winpower

// readACPower cannot detect the source off Windows, so it reports AC (port contract).
func readACPower() (bool, error) { return true, nil }
