//go:build !windows

package audiocapture

// PlatformSource returns the capture source of this OS: none outside Windows.
func PlatformSource() Source { return unavailableSource{} }
