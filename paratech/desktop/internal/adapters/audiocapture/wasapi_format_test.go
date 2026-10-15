package audiocapture

import (
	"encoding/binary"
	"errors"
	"testing"
)

// The byte offsets match the packed WAVEFORMATEX / WAVEFORMATEXTENSIBLE of mmreg.h.
func TestWaveFormatOffsetsMatchTheABI(t *testing.T) {
	tests := []struct {
		name      string
		got, want int
	}{
		{"wFormatTag", wfxOffTag, 0},
		{"nChannels", wfxOffChannels, 2},
		{"nSamplesPerSec", wfxOffSampleRate, 4},
		{"nAvgBytesPerSec", wfxOffByteRate, 8},
		{"nBlockAlign", wfxOffBlockAlign, 12},
		{"wBitsPerSample", wfxOffBits, 14},
		{"cbSize", wfxOffExtraSize, 16},
		{"sizeof(WAVEFORMATEX)", waveFormatExSize, 18},
		{"wValidBitsPerSample", wfxOffValidBits, 18},
		{"dwChannelMask", wfxOffChannelMask, 20},
		{"SubFormat", wfxOffSubFormat, 24},
		{"cbSize of WAVEFORMATEXTENSIBLE", waveFormatExtraSize, 22},
		{"sizeof(WAVEFORMATEXTENSIBLE)", waveFormatExSize + waveFormatExtraSize, extensibleFmtLen},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s at %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

// waveFormatEx builds a WAVEFORMATEX; ext adds the extensible tail with sub-format tag sub and validBits.
func waveFormatEx(tag, ch uint16, rate uint32, bits uint16, ext bool, sub uint32, validBits uint16) []byte {
	le := binary.LittleEndian
	block := ch * bits / 8
	b := make([]byte, waveFormatExSize)
	le.PutUint16(b[wfxOffTag:], tag)
	le.PutUint16(b[wfxOffChannels:], ch)
	le.PutUint32(b[wfxOffSampleRate:], rate)
	le.PutUint32(b[wfxOffByteRate:], rate*uint32(block))
	le.PutUint16(b[wfxOffBlockAlign:], block)
	le.PutUint16(b[wfxOffBits:], bits)
	if !ext {
		return b
	}
	le.PutUint16(b[wfxOffExtraSize:], waveFormatExtraSize)
	b = append(b, make([]byte, waveFormatExtraSize)...)
	le.PutUint16(b[wfxOffValidBits:], validBits)
	le.PutUint32(b[wfxOffChannelMask:], 0x3)
	le.PutUint32(b[wfxOffSubFormat:], sub)
	copy(b[wfxOffSubFormat+4:], subFormatTail())
	return b
}

func TestParseWaveFormat(t *testing.T) {
	f32 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	s16 := Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}
	s24in32 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32}

	badTail := waveFormatEx(tagExtensible, 2, 48000, 32, true, tagFloat, 32)
	badTail[len(badTail)-1] ^= 0xff
	badBlock := waveFormatEx(tagPCM, 2, 44100, 16, false, 0, 0)
	binary.LittleEndian.PutUint16(badBlock[wfxOffBlockAlign:], 3)
	shortExt := waveFormatEx(tagExtensible, 2, 48000, 32, true, tagFloat, 32)
	binary.LittleEndian.PutUint16(shortExt[wfxOffExtraSize:], 10)

	tests := []struct {
		name string
		b    []byte
		want Format
		ok   bool
	}{
		{name: "extensible float (the usual mix format)", b: waveFormatEx(tagExtensible, 2, 48000, 32, true, tagFloat, 32), want: f32, ok: true},
		{name: "extensible pcm", b: waveFormatEx(tagExtensible, 2, 44100, 16, true, tagPCM, 16), want: s16, ok: true},
		{name: "24 valid bits in 32-bit samples", b: waveFormatEx(tagExtensible, 2, 48000, 32, true, tagPCM, 24), want: s24in32, ok: true},
		{name: "plain float", b: waveFormatEx(tagFloat, 2, 48000, 32, false, 0, 0), want: f32, ok: true},
		{name: "plain pcm", b: waveFormatEx(tagPCM, 2, 44100, 16, false, 0, 0), want: s16, ok: true},
		{name: "short", b: make([]byte, 10)},
		{name: "17 bytes", b: waveFormatEx(tagPCM, 2, 44100, 16, false, 0, 0)[:17]},
		{name: "extensible without its tail", b: waveFormatEx(tagExtensible, 2, 48000, 32, false, 0, 0)},
		{name: "extensible with a short cbSize", b: shortExt},
		{name: "unknown sub-format GUID", b: badTail},
		{name: "more valid bits than the sample", b: waveFormatEx(tagExtensible, 2, 48000, 16, true, tagPCM, 24)},
		{name: "unknown tag", b: waveFormatEx(0x0055, 2, 44100, 16, false, 0, 0)},
		{name: "zero channels", b: waveFormatEx(tagPCM, 0, 44100, 16, false, 0, 0)},
		{name: "too many channels", b: waveFormatEx(tagFloat, 16, 48000, 32, false, 0, 0)},
		{name: "8-bit pcm", b: waveFormatEx(tagPCM, 2, 44100, 8, false, 0, 0)},
		{name: "rate too high", b: waveFormatEx(tagFloat, 2, 384000, 32, false, 0, 0)},
		{name: "inconsistent block align", b: badBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWaveFormat(tt.b)
			if !tt.ok {
				if !errors.Is(err, ErrFormatRejected) || !errors.Is(err, ErrDevice) {
					t.Fatalf("parseWaveFormat = %v, %v; want ErrFormatRejected", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseWaveFormat = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

// Only the bytes Windows allocated (18 + cbSize) are read, and never more than a WAVEFORMATEXTENSIBLE.
func TestWaveFormatReadLen(t *testing.T) {
	tests := []struct {
		cbSize uint16
		want   int
	}{{0, 18}, {2, 20}, {22, 40}, {0xFFFF, 40}}
	for _, tt := range tests {
		head := make([]byte, waveFormatExSize)
		binary.LittleEndian.PutUint16(head[wfxOffExtraSize:], tt.cbSize)
		if got := waveFormatReadLen(head); got != tt.want {
			t.Errorf("cbSize %d: read %d bytes, want %d", tt.cbSize, got, tt.want)
		}
	}
}

func TestEncodeWaveFormatRoundTrip(t *testing.T) {
	formats := processFormats()
	if len(formats) != 2 || formats[0] != (Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}) ||
		formats[1] != (Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}) {
		t.Fatalf("process loopback formats %v, want 48 kHz float32 stereo, then 44.1 kHz s16 stereo", formats)
	}
	for _, f := range formats {
		b := encodeWaveFormat(f)
		if len(b) != waveFormatExSize || waveFormatReadLen(b) != waveFormatExSize {
			t.Fatalf("%s: %d bytes", f, len(b))
		}
		got, err := parseWaveFormat(b)
		if err != nil || got != f {
			t.Fatalf("%s: round trip = %v, %v", f, got, err)
		}
	}
}
