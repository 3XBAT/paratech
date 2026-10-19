//go:build !windows || !(amd64 || arm64)

package audiocapture

// platformApps is the app list of this OS: none outside 64-bit Windows. Every call fails with ErrUnavailable, so the
// source picker offers only the whole computer.
func platformApps() appPlatform { return unavailableApps{} }

type unavailableApps struct{}

func (unavailableApps) snapshot(bool) (snapshot, error)      { return snapshot{}, ErrUnavailable }
func (unavailableApps) inspect(uint32) (procIdentity, error) { return procIdentity{}, ErrUnavailable }
func (unavailableApps) icon(string) []byte                   { return nil }
func (unavailableApps) describe(string) (string, string)     { return "", "" }
