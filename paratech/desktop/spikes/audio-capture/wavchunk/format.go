// Package wavchunk writes interleaved audio frames into closed WAV chunk files and validates them
// (spike of TASK-015, PRD section 6.3: a crash loses at most the latest chunk).
//
// Chunk strategy: every chunk is written to a temporary name (chunk-000001.wav.partial) with a placeholder
// header, and only after its final header is patched in and the file is fsynced it is renamed to
// chunk-000001.wav. A file with the final name is therefore always complete; a killed recorder leaves at most one
// .partial file, which Validate reports as incomplete. Patching in place without a rename would make a reader
// unable to tell a finished chunk from one that is still being written.
package wavchunk

import (
	"errors"
	"fmt"
	"time"
)

const (
	minSampleRate = 1000
	maxSampleRate = 384000
	maxChannels   = 32
)

// ErrInvalidFormat means the format cannot be written as WAV by this package.
var ErrInvalidFormat = errors.New("wavchunk: invalid format")

// Format describes interleaved frames: SampleRate frames per second, each with Channels samples of
// BitsPerSample bits, integer PCM or IEEE float.
type Format struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
	Float         bool
}

// Validate reports whether the format is supported.
func (f Format) Validate() error {
	switch {
	case f.SampleRate < minSampleRate || f.SampleRate > maxSampleRate:
		return fmt.Errorf("%w: sample rate %d", ErrInvalidFormat, f.SampleRate)
	case f.Channels == 0 || f.Channels > maxChannels:
		return fmt.Errorf("%w: %d channels", ErrInvalidFormat, f.Channels)
	case f.Float && f.BitsPerSample != 32 && f.BitsPerSample != 64:
		return fmt.Errorf("%w: %d-bit float", ErrInvalidFormat, f.BitsPerSample)
	case !f.Float && f.BitsPerSample != 8 && f.BitsPerSample != 16 && f.BitsPerSample != 24 && f.BitsPerSample != 32:
		return fmt.Errorf("%w: %d-bit PCM", ErrInvalidFormat, f.BitsPerSample)
	}
	return nil
}

// BlockAlign is the size of one frame in bytes.
func (f Format) BlockAlign() int { return int(f.Channels) * int(f.BitsPerSample) / 8 }

// ByteRate is the number of bytes per second.
func (f Format) ByteRate() uint32 { return f.SampleRate * uint32(f.BlockAlign()) } //nolint:gosec // BlockAlign <= 32*64/8 after Validate

// Duration converts a frame count to time, without wall clock.
func (f Format) Duration(frames int64) time.Duration {
	if f.SampleRate == 0 {
		return 0
	}
	return time.Duration(frames) * time.Second / time.Duration(f.SampleRate)
}

func (f Format) String() string {
	kind := "PCM"
	if f.Float {
		kind = "float"
	}
	return fmt.Sprintf("%d Hz, %d ch, %d-bit %s", f.SampleRate, f.Channels, f.BitsPerSample, kind)
}
