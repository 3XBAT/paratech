//go:build !windows || !(amd64 || arm64)

package audiocapture

import "context"

// PlatformSource returns the capture source of this OS: none outside 64-bit Windows.
func PlatformSource(SourceConfig) Source { return unavailableSource{} }

// Supported reports whether this build captures audio.
func Supported() bool { return false }

// unavailableSource is the Source of a build without capture.
type unavailableSource struct{}

func (unavailableSource) Open(context.Context, Target, Observer) (Stream, error) {
	return nil, ErrUnavailable
}
