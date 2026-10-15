package audiocapture

import (
	"encoding/binary"
	"fmt"
)

// WAVEFORMATEX (mmreg.h, byte packed) and the WAVEFORMATEXTENSIBLE tail: byte offsets of the fields. WASAPI formats
// are read and written as bytes, because a Go struct cannot reproduce the packed 18-byte layout.
const (
	wfxOffTag         = 0  // wFormatTag
	wfxOffChannels    = 2  // nChannels
	wfxOffSampleRate  = 4  // nSamplesPerSec
	wfxOffByteRate    = 8  // nAvgBytesPerSec
	wfxOffBlockAlign  = 12 // nBlockAlign
	wfxOffBits        = 14 // wBitsPerSample
	wfxOffExtraSize   = 16 // cbSize
	wfxOffValidBits   = 18 // Samples.wValidBitsPerSample (extensible)
	wfxOffChannelMask = 20 // dwChannelMask (extensible)
	wfxOffSubFormat   = 24 // SubFormat GUID (extensible)

	waveFormatExSize    = 18 // sizeof(WAVEFORMATEX)
	waveFormatExtraSize = 22 // cbSize of a WAVEFORMATEXTENSIBLE
)

// waveFormatReadLen returns how many bytes of a WAVEFORMATEX whose first 18 bytes are head to read: the
// WAVEFORMATEXTENSIBLE part at most, never more than the 18 + cbSize bytes Windows allocated.
func waveFormatReadLen(head []byte) int {
	extra := int(binary.LittleEndian.Uint16(head[wfxOffExtraSize : wfxOffExtraSize+2]))
	return waveFormatExSize + min(extra, waveFormatExtraSize)
}

// parseWaveFormat decodes a format WASAPI reports (the mix format of an endpoint): WAVEFORMATEX or
// WAVEFORMATEXTENSIBLE with integer PCM or IEEE float samples that the chunk writer accepts. Everything else is
// ErrFormatRejected, so a malformed or exotic format never reaches the capture loop.
func parseWaveFormat(b []byte) (Format, error) {
	if len(b) < waveFormatExSize {
		return Format{}, fmt.Errorf("%w: WAVEFORMATEX of %d bytes", ErrFormatRejected, len(b))
	}
	f, err := decodeWaveFormat(b)
	if err != nil {
		return Format{}, fmt.Errorf("%w: %w", ErrFormatRejected, err)
	}
	if binary.LittleEndian.Uint16(b[wfxOffTag:]) == tagExtensible {
		if valid := binary.LittleEndian.Uint16(b[wfxOffValidBits:]); valid > f.BitsPerSample {
			return Format{}, fmt.Errorf("%w: %d valid bits in %d-bit samples", ErrFormatRejected, valid, f.BitsPerSample)
		}
	}
	return f, nil
}

// encodeWaveFormat returns a plain WAVEFORMATEX (cbSize 0) for f, the form process loopback Initialize accepts.
func encodeWaveFormat(f Format) []byte {
	le := binary.LittleEndian
	tag := uint16(tagPCM)
	if f.Float {
		tag = tagFloat
	}
	b := make([]byte, 0, waveFormatExSize)
	b = le.AppendUint16(b, tag)
	b = le.AppendUint16(b, f.Channels)
	b = le.AppendUint32(b, f.SampleRate)
	b = le.AppendUint32(b, f.ByteRate())
	b = le.AppendUint16(b, uint16(f.BlockAlign())) //nolint:gosec // the formats of processFormats
	b = le.AppendUint16(b, f.BitsPerSample)
	return le.AppendUint16(b, 0)
}

// processFormats are requested in order from process loopback, which has no mix format (GetMixFormat is E_NOTIMPL,
// ADR-0002 Decision 6): 48 kHz float32 stereo, accepted on 19045, then 44.1 kHz 16-bit stereo, the format of
// Microsoft's sample.
func processFormats() []Format {
	return []Format{
		{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true},
		{SampleRate: 44100, Channels: 2, BitsPerSample: 16},
	}
}
