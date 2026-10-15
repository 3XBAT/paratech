package audiocapture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestReadHeader(t *testing.T) {
	s16 := Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}
	f32 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	s24 := Format{SampleRate: 48000, Channels: 1, BitsPerSample: 24}
	s32x6 := Format{SampleRate: 96000, Channels: 6, BitsPerSample: 32}

	good := wavFile(fmtBody(s16, false), nil, tone(s16, 1000, 0.5, 0, 100))
	ours := append(encodeHeader(f32, 10, true), tone(f32, 1000, 0.5, 0, 10)...)
	odd := append(append(encodeHeader(s24, 3, true), tone(s24, 1000, 0.5, 0, 3)...), 0)
	list := []byte("LIST\x05\x00\x00\x00INFOx\x00") // odd size, padded
	placeholder := append(encodeHeader(s16, 0, false), tone(s16, 1000, 0.5, 0, 10)...)

	bigTag := fmtBody(s16, false)
	binary.LittleEndian.PutUint16(bigTag[0:2], 0x0055) // MP3
	badAlign := fmtBody(s16, false)
	binary.LittleEndian.PutUint16(badAlign[12:14], 3)
	eight := Format{SampleRate: 8000, Channels: 1, BitsPerSample: 8}
	badExt := fmtBody(f32, true)
	badExt[30] ^= 0xff

	tests := []struct {
		name string
		file []byte
		want chunkInfo
		bad  bool
	}{
		{name: "pcm 16 stereo", file: good, want: chunkInfo{s16, 100}},
		{name: "float with fact chunk", file: ours, want: chunkInfo{f32, 10}},
		{name: "odd data with pad byte", file: odd, want: chunkInfo{s24, 3}},
		{name: "extensible float", file: wavFile(fmtBody(f32, true), nil, tone(f32, 1000, 0.5, 0, 7)), want: chunkInfo{f32, 7}},
		{name: "extensible pcm 32 six channels", file: wavFile(fmtBody(s32x6, true), nil, tone(s32x6, 1000, 0.5, 0, 5)), want: chunkInfo{s32x6, 5}},
		{name: "unknown chunk before data", file: wavFile(fmtBody(s16, false), list, tone(s16, 1000, 0.5, 0, 4)), want: chunkInfo{s16, 4}},
		{name: "empty data", file: wavFile(fmtBody(s16, false), nil, nil), want: chunkInfo{s16, 0}},
		{name: "truncated", file: good[:len(good)-1], bad: true},
		{name: "placeholder header of a killed writer", file: placeholder, bad: true},
		{name: "garbage", file: bytes.Repeat([]byte{0x5a}, 300), bad: true},
		{name: "too short", file: []byte("RIFF"), bad: true},
		{name: "no data chunk", file: append([]byte("RIFF\x04\x00\x00\x00"), "WAVE"...), bad: true},
		{name: "partial frame", file: wavFile(fmtBody(s16, false), nil, []byte{1, 2, 3}), bad: true},
		{name: "unsupported tag", file: wavFile(bigTag, nil, tone(s16, 1000, 0.5, 0, 4)), bad: true},
		{name: "inconsistent block align", file: wavFile(badAlign, nil, tone(s16, 1000, 0.5, 0, 4)), bad: true},
		{name: "8-bit pcm", file: wavFile(fmtBody(eight, false), nil, make([]byte, 8)), bad: true},
		{name: "unknown sub-format", file: wavFile(badExt, nil, tone(f32, 1000, 0.5, 0, 4)), bad: true},
		{name: "data before fmt", file: []byte("RIFF\x0c\x00\x00\x00WAVEdata\x00\x00\x00\x00"), bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readHeader(bytes.NewReader(tt.file), int64(len(tt.file)))
			if tt.bad {
				if !errors.Is(err, errNotAudio) {
					t.Fatalf("readHeader = %+v, %v; want errNotAudio", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("readHeader = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

// A reader that fails is an I/O problem, not a broken chunk.
func TestReadHeaderReaderFailure(t *testing.T) {
	file := wavFile(fmtBody(Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}, false), nil, make([]byte, 20))
	_, err := readHeader(bytes.NewReader(file[:20]), int64(len(file)))
	if err == nil || errors.Is(err, errNotAudio) {
		t.Fatalf("readHeader on a short reader = %v, want an I/O error", err)
	}
}

func TestFormatValidate(t *testing.T) {
	tests := []struct {
		f  Format
		ok bool
	}{
		{Format{48000, 2, 32, true}, true},
		{Format{44100, 2, 16, false}, true},
		{Format{8000, 1, 24, false}, true},
		{Format{192000, 8, 32, false}, true},
		{Format{7999, 1, 16, false}, false},
		{Format{192001, 1, 16, false}, false},
		{Format{48000, 0, 16, false}, false},
		{Format{48000, 9, 16, false}, false},
		{Format{48000, 2, 8, false}, false},
		{Format{48000, 2, 64, true}, false},
		{Format{48000, 2, 16, true}, false},
	}
	for _, tt := range tests {
		if err := tt.f.Validate(); (err == nil) != tt.ok {
			t.Errorf("%s: Validate = %v, want ok %v", tt.f, err, tt.ok)
		}
	}
}
