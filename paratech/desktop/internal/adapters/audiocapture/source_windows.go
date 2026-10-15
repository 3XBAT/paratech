//go:build windows

package audiocapture

// PlatformSource returns the capture source of this OS. The WASAPI source (ADR-0002) replaces this stand-in.
func PlatformSource() Source { return unavailableSource{} }
