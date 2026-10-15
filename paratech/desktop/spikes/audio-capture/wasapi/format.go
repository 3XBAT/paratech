package wasapi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// WAVEFORMATEX layout (mmreg.h, byte packed): wFormatTag, nChannels (WORD); nSamplesPerSec, nAvgBytesPerSec
// (DWORD); nBlockAlign, wBitsPerSample, cbSize (WORD) = 18 bytes. WAVEFORMATEXTENSIBLE adds Samples (WORD),
// dwChannelMask (DWORD) and SubFormat (GUID) = 40 bytes, cbSize 22.
const (
	waveFormatExSize  = 18
	extensibleExtra   = 22
	waveTagPCM        = 0x0001 // WAVE_FORMAT_PCM
	waveTagFloat      = 0x0003 // WAVE_FORMAT_IEEE_FLOAT
	waveTagExtensible = 0xFFFE // WAVE_FORMAT_EXTENSIBLE
	subFormatOffset   = 24
)

// subFormatTail is the GUID tail shared by KSDATAFORMAT_SUBTYPE_PCM and _IEEE_FLOAT
// (DEFINE_WAVEFORMATEX_GUID: Data1 = tag, Data2 = 0x0000, Data3 = 0x0010, Data4 = 80 00 00 aa 00 38 9b 71),
// as stored in memory after Data1.
var subFormatTail = []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}

// errFormat means a WAVEFORMATEX describes something this spike does not write.
var errFormat = errors.New("wasapi: unsupported stream format")

// waveFormatLen returns the full size of the WAVEFORMATEX whose first 18 bytes are head (18 + cbSize).
func waveFormatLen(head []byte) int {
	return waveFormatExSize + int(binary.LittleEndian.Uint16(head[16:18]))
}

// parseWaveFormat decodes a WAVEFORMATEX or WAVEFORMATEXTENSIBLE with integer PCM or IEEE float samples.
func parseWaveFormat(b []byte) (Format, error) {
	le := binary.LittleEndian
	if len(b) < waveFormatExSize {
		return Format{}, fmt.Errorf("%w: %d bytes", errFormat, len(b))
	}

	tag := uint32(le.Uint16(b[0:2]))
	if tag == waveTagExtensible {
		if len(b) < waveFormatExSize+extensibleExtra || int(le.Uint16(b[16:18])) < extensibleExtra {
			return Format{}, fmt.Errorf("%w: short WAVEFORMATEXTENSIBLE", errFormat)
		}
		if !bytes.Equal(b[subFormatOffset+4:subFormatOffset+16], subFormatTail) {
			return Format{}, fmt.Errorf("%w: sub-format % x", errFormat, b[subFormatOffset:subFormatOffset+16])
		}
		tag = le.Uint32(b[subFormatOffset : subFormatOffset+4])
	}

	f := Format{
		Channels:      le.Uint16(b[2:4]),
		SampleRate:    le.Uint32(b[4:8]),
		BitsPerSample: le.Uint16(b[14:16]),
	}
	switch tag {
	case waveTagPCM:
	case waveTagFloat:
		f.Float = true
	default:
		return Format{}, fmt.Errorf("%w: tag 0x%04x", errFormat, tag)
	}

	if f.Channels == 0 || f.SampleRate == 0 || f.BitsPerSample == 0 || f.BitsPerSample%8 != 0 {
		return Format{}, fmt.Errorf("%w: %s", errFormat, f)
	}
	if int(le.Uint16(b[12:14])) != f.BlockAlign() {
		return Format{}, fmt.Errorf("%w: block align %d for %s", errFormat, le.Uint16(b[12:14]), f)
	}
	return f, nil
}

// encodeWaveFormat returns a plain WAVEFORMATEX (cbSize 0), the form process loopback Initialize accepts.
func encodeWaveFormat(f Format) []byte {
	le := binary.LittleEndian
	tag := uint16(waveTagPCM)
	if f.Float {
		tag = waveTagFloat
	}
	block := uint16(f.BlockAlign()) //nolint:gosec // formats are the constants of processFormats

	b := make([]byte, 0, waveFormatExSize)
	b = le.AppendUint16(b, tag)
	b = le.AppendUint16(b, f.Channels)
	b = le.AppendUint32(b, f.SampleRate)
	b = le.AppendUint32(b, f.SampleRate*uint32(block))
	b = le.AppendUint16(b, block)
	b = le.AppendUint16(b, f.BitsPerSample)
	b = le.AppendUint16(b, 0)
	return b
}

// processFormats are tried in order for process loopback, where GetMixFormat returns E_NOTIMPL: float32 as OBS
// uses, then 44.1 kHz 16-bit stereo, which Microsoft's sample uses.
func processFormats() []Format {
	return []Format{
		{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true},
		{SampleRate: 44100, Channels: 2, BitsPerSample: 16},
	}
}
