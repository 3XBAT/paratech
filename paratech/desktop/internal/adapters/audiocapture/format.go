package audiocapture

import (
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

// Supported stream formats: what WASAPI shared mode delivers in practice (ADR-0002 Decision 6) with a margin.
const (
	minSampleRate = 8000
	maxSampleRate = 192000
	maxChannels   = 8
)

// ErrInvalidFormat means a stream format cannot be written or read as a chunk.
var ErrInvalidFormat = errors.New("audiocapture: unsupported audio format")

// Format describes interleaved frames: SampleRate frames per second, each with Channels samples of BitsPerSample
// bits, signed integer PCM or IEEE float.
type Format struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
	Float         bool
}

// Validate accepts 8..192 kHz, 1..8 channels, 16/24/32-bit PCM and 32-bit float.
func (f Format) Validate() error {
	switch {
	case f.SampleRate < minSampleRate || f.SampleRate > maxSampleRate:
		return fmt.Errorf("%w: sample rate %d", ErrInvalidFormat, f.SampleRate)
	case f.Channels == 0 || f.Channels > maxChannels:
		return fmt.Errorf("%w: %d channels", ErrInvalidFormat, f.Channels)
	case f.Float && f.BitsPerSample != 32:
		return fmt.Errorf("%w: %d-bit float", ErrInvalidFormat, f.BitsPerSample)
	case !f.Float && f.BitsPerSample != 16 && f.BitsPerSample != 24 && f.BitsPerSample != 32:
		return fmt.Errorf("%w: %d-bit PCM", ErrInvalidFormat, f.BitsPerSample)
	}
	return nil
}

// BlockAlign is the size of one frame in bytes.
func (f Format) BlockAlign() int { return int(f.Channels) * int(f.BitsPerSample) / 8 }

// ByteRate is the number of bytes per second.
func (f Format) ByteRate() uint32 { return f.SampleRate * uint32(f.BlockAlign()) } //nolint:gosec // BlockAlign <= 32 after Validate

func (f Format) String() string { return f.Stream().String() }

// Stream is the format as the domain describes it, for the diagnostics.
func (f Format) Stream() values.StreamFormat {
	return values.StreamFormat{SampleRate: f.SampleRate, Channels: f.Channels, BitsPerSample: f.BitsPerSample, Float: f.Float}
}

// framesFor converts a duration to a frame count of rate, rounding down; the product cannot overflow for durations
// up to days.
func framesFor(d time.Duration, rate uint32) int64 {
	if d <= 0 {
		return 0
	}
	r := int64(rate)
	return int64(d/time.Second)*r + int64(d%time.Second)*r/int64(time.Second)
}

// durationOf converts a frame count of rate to time, without wall clock.
func durationOf(frames int64, rate uint32) time.Duration {
	if rate == 0 || frames <= 0 {
		return 0
	}
	r := int64(rate)
	return time.Duration(frames/r)*time.Second + time.Duration(frames%r)*time.Second/time.Duration(r)
}

// TrackRate is the sample rate of the final track.
const TrackRate = 16000

// trackFormat is the format of the final track: whisper resamples any input to 16 kHz mono anyway, and 16-bit keeps
// 12 hours at about 1.4 GB, far below the 4 GiB limit of a WAV file.
func trackFormat() Format { return Format{SampleRate: TrackRate, Channels: 1, BitsPerSample: 16} }
