package recording

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// Synthetic MPEG audio frame headers.
var (
	cbr128 = [4]byte{0xFF, 0xFB, 0x90, 0x64} // MPEG-1 layer III, 128 kbit/s, 44.1 kHz: 417 bytes, 1152 samples
	low8   = [4]byte{0xFF, 0xE3, 0x18, 0xC4} // MPEG-2.5 layer III, 8 kbit/s, 8 kHz, mono: 72 bytes, 576 samples
	low16  = [4]byte{0xFF, 0xE3, 0x28, 0xC4} // the same at 16 kbit/s: 144 bytes
	mpeg2  = [4]byte{0xFF, 0xF3, 0x90, 0x64} // MPEG-2 layer III, 80 kbit/s, 22.05 kHz: another stream
)

// mp3Frames is n frames of hdr with silent (zero) bodies.
func mp3Frames(hdr [4]byte, n int) []byte {
	h, ok := parseMP3Header(hdr[:])
	if !ok || h.length < 4 {
		panic("not a frame header")
	}
	frame := append(hdr[:], make([]byte, h.length-4)...)
	return bytes.Repeat(frame, n)
}

// mixedFrames is one frame of each header in turn.
func mixedFrames(hdrs ...[4]byte) []byte {
	var b []byte
	for _, h := range hdrs {
		b = append(b, mp3Frames(h, 1)...)
	}
	return b
}

// xingFrame is a 128 kbit/s frame with a Xing header (after 32 bytes of side information) that counts frames.
func xingFrame(frames uint32) []byte {
	b := mp3Frames(cbr128, 1)
	copy(b[36:], "Xing")
	binary.BigEndian.PutUint32(b[40:], 1)
	binary.BigEndian.PutUint32(b[44:], frames)
	return b
}

// vbriFrame is a 128 kbit/s frame with a VBRI header 32 bytes after the frame header.
func vbriFrame(frames uint32) []byte {
	b := mp3Frames(cbr128, 1)
	copy(b[36:], "VBRI")
	binary.BigEndian.PutUint32(b[50:], frames)
	return b
}

func TestParseMP3Header(t *testing.T) {
	tests := []struct {
		name                   string
		hdr                    []byte
		bitrate, rate, samples uint32
		length                 int
	}{
		{"mpeg-1 layer III", cbr128[:], 128000, 44100, 1152, 417},
		{"with padding", []byte{0xFF, 0xFB, 0x92, 0x64}, 128000, 44100, 1152, 418},
		{"mpeg-2.5 layer III", low8[:], 8000, 8000, 576, 72},
		{"mpeg-2 layer III", mpeg2[:], 80000, 22050, 576, 261},
		{"mpeg-1 layer I", []byte{0xFF, 0xFF, 0x90, 0x64}, 288000, 44100, 384, 312},
		{"mpeg-1 layer II", []byte{0xFF, 0xFD, 0x90, 0x64}, 160000, 44100, 1152, 522},
		{"mpeg-2 layer II", []byte{0xFF, 0xF5, 0x90, 0x64}, 80000, 22050, 1152, 522},
		{"free format", []byte{0xFF, 0xFB, 0x04, 0x64}, 0, 48000, 1152, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, ok := parseMP3Header(tc.hdr)
			if !ok || h.bitrate != tc.bitrate || h.rate != tc.rate || h.samples != tc.samples || h.length != tc.length {
				t.Fatalf("parseMP3Header = %+v, %v", h, ok)
			}
		})
	}
	for _, bad := range [][]byte{{0xFF, 0xF1, 0x50, 0x80}, {0xFF, 0xFB, 0xF0, 0x64}, {0xFF, 0xFB, 0x9C, 0x64}, {0xFF, 0xEB, 0x90, 0x64}, {0xFF, 0xFB}} {
		if h, ok := parseMP3Header(bad); ok {
			t.Errorf("parseMP3Header(% x) = %+v, ok", bad, h)
		}
	}
}

// An MP3 is refused before the copy when its headers say it is too long; otherwise its frames are to be counted.
func TestDetectAudioMP3(t *testing.T) {
	const huge = int64(16_000_000) // 16000 s at 8 kbit/s
	counting := ImportedAudio{Format: FormatMP3, counting: true}
	tests := []struct {
		name       string
		head, tail []byte
		size       int64
		err        error
	}{
		{"xing over four hours", xingFrame(600_000), nil, 1 << 30, ErrAudioTooLong},
		{"xing within", xingFrame(1000), nil, 1 << 30, nil},
		{"info of a cbr file over four hours", bytes.Replace(xingFrame(600_000), []byte("Xing"), []byte("Info"), 1), nil, 1 << 30, ErrAudioTooLong},
		{"vbri over four hours", vbriFrame(600_000), nil, 1 << 30, ErrAudioTooLong},
		// The size over the bitrate is no estimate: the same head and tail may hold a VBR file without a header.
		{"cbr-like head and tail of a large file", mp3Frames(low8, 900), mp3Frames(low8, 1800), huge, nil},
		{"cbr within", mp3Frames(low8, 900), mp3Frames(low8, 1800), 1_000_000, nil},
		{"vbr without a header", bytes.Repeat(mixedFrames(low8, low16), 300), mp3Frames(low8, 1800), huge, nil},
		{"vbr at the end only", mp3Frames(low8, 900), bytes.Repeat(mixedFrames(low8, low16), 600), huge, nil},
		{"no frames in the tail", mp3Frames(low8, 900), []byte("TAG and nothing else"), huge, nil},
		{"one frame", mp3Frames(low8, 1), mp3Frames(low8, 1800), huge, nil},
		{"free format", []byte{0xFF, 0xFB, 0x04, 0x64, 0, 0, 0, 0}, nil, huge, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectAudio(tc.head, tc.tail, tc.size)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("DetectAudio = %+v, %v; want %v", got, err, tc.err)
				}
				return
			}
			if err != nil || got != counting {
				t.Fatalf("DetectAudio = %+v, %v; want the frames counted", got, err)
			}
		})
	}
}

// A VBR file without a Xing/VBRI header that starts and ends in silence at the lowest bitrate: its size over that
// bitrate is over four hours, its frames are 2h15m36s. It is accepted, and the count is its duration.
func TestMP3SilentEdgesWithoutVBRHeader(t *testing.T) {
	parts := [][]byte{mp3Frames(low8, 1000), mp3Frames(low16, 110_000), mp3Frames(low8, 2000)}
	var size int64
	for _, p := range parts {
		size += int64(len(p))
	}
	if ratio(uint64(size)*8, 8000) <= MaxImportedDuration {
		t.Fatalf("the fixture must look longer than the cap at 8 kbit/s")
	}
	head, tail := parts[0][:ImportHeadBytes], parts[2][len(parts[2])-ImportTailBytes:]
	audio, err := DetectAudio(head, tail, size)
	if err != nil || !audio.NeedsFrameCount() {
		t.Fatalf("DetectAudio = %+v, %v; want the frames counted", audio, err)
	}
	c := NewMP3FrameCounter(audio)
	for _, p := range parts {
		_, _ = c.Write(p)
	}
	got, err := audio.Counted(c)
	if want := 113_000 * 72 * time.Millisecond; err != nil || got.Duration != want {
		t.Fatalf("Counted = %+v, %v; want %v", got, err, want)
	}
}

func TestMP3FrameCounter(t *testing.T) {
	falseSyncs := bytes.Repeat(cbr128[:], 10)
	tag := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, byte(len(falseSyncs))}, falseSyncs...)
	tests := []struct {
		name string
		from int64
		data []byte
		want time.Duration
		err  error
	}{
		{"cbr", 0, mp3Frames(cbr128, 10), ratio(10*1152, 44100), nil},
		{"vbr without a header", 0, bytes.Repeat(mixedFrames(low8, low16), 5), 720 * time.Millisecond, nil},
		{"after an id3 tag with syncs inside", int64(len(tag)), append(tag, mp3Frames(low8, 3)...), 216 * time.Millisecond, nil},
		{"junk between frames", 0, bytes.Join([][]byte{mp3Frames(low8, 3), []byte("junk\xFF\x00\xFF\xFFjunk\xFF"), mp3Frames(low8, 2)}, nil),
			360 * time.Millisecond, nil},
		{"a frame of another stream", 0, bytes.Join([][]byte{mp3Frames(low8, 2), mp3Frames(mpeg2, 1), mp3Frames(low8, 2)}, nil),
			288 * time.Millisecond, nil},
		{"junk before the first frame", 0, append([]byte("\x00\x01\xFF\xFF\xFF\x00abc"), mp3Frames(low8, 2)...), 144 * time.Millisecond, nil},
		// A layer I header in junk covers the first three frames: they are lost, the stream is found again after them.
		{"a false sync before the frames", 0, append([]byte{0xFF, 0xFF, 0x61, 0x62}, mp3Frames(low8, 10)...), 504 * time.Millisecond, nil},
		{"one frame only", 0, mp3Frames(low8, 1), 0, ErrAudioDurationUnknown},
		{"a cut last frame", 0, mp3Frames(low8, 2)[:100], 144 * time.Millisecond, nil},
		{"free format only", 0, bytes.Repeat([]byte{0xFF, 0xFB, 0x04, 0x64, 0, 0}, 50), 0, ErrAudioDurationUnknown},
		{"the tag is all the file", 1000, mp3Frames(low8, 3), 0, ErrAudioDurationUnknown},
		{"nothing", 0, nil, 0, ErrAudioDurationUnknown},
	}
	for _, tc := range tests {
		for _, chunk := range []int{len(tc.data) + 1, 1, 7, 4096} {
			audio := ImportedAudio{Format: FormatMP3, counting: true, countFrom: tc.from}
			c := NewMP3FrameCounter(audio)
			for b := tc.data; len(b) > 0; {
				k := min(chunk, len(b))
				if n, err := c.Write(b[:k]); n != k || err != nil {
					t.Fatalf("%s: Write = %d, %v", tc.name, n, err)
				}
				b = b[k:]
			}
			got, err := audio.Counted(c)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("%s in chunks of %d: Counted = %+v, %v; want %v", tc.name, chunk, got, err, tc.err)
				}
				continue
			}
			if err != nil || got != (ImportedAudio{Format: FormatMP3, Duration: tc.want}) {
				t.Fatalf("%s in chunks of %d: Counted = %+v, %v; want %v", tc.name, chunk, got, err, tc.want)
			}
		}
	}
}

// The count is the cap of an MP3 without a usable header: four hours pass, a frame more does not.
func TestMP3FrameCounterCap(t *testing.T) {
	block := mp3Frames(low8, 1000) // 72 s
	for _, tc := range []struct {
		blocks int
		err    error
	}{{200, nil}, {201, ErrAudioTooLong}} {
		audio := ImportedAudio{Format: FormatMP3, counting: true}
		c := NewMP3FrameCounter(audio)
		for range tc.blocks {
			_, _ = c.Write(block)
		}
		got, err := audio.Counted(c)
		if !errors.Is(err, tc.err) || (err == nil && got.Duration != MaxImportedDuration) {
			t.Fatalf("%d blocks: Counted = %+v, %v; want %v", tc.blocks, got, err, tc.err)
		}
	}
}

func TestCountedAndNewImported(t *testing.T) {
	wav := ImportedAudio{Format: FormatWAV, Duration: time.Minute}
	if got, err := wav.Counted(nil); err != nil || got != wav {
		t.Fatalf("Counted of a WAV = %+v, %v", got, err)
	}
	mp3 := ImportedAudio{Format: FormatMP3, counting: true}
	if _, err := mp3.Counted(nil); !errors.Is(err, ErrAudioDurationUnknown) {
		t.Fatalf("Counted without a counter = %v", err)
	}

	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	spec := ImportSpec{ID: "imp-1", StartedAt: t0.Add(-time.Hour), Audio: mp3, SHA256: string(bytes.Repeat([]byte("ab"), 32))}
	if _, err := NewImported(spec, t0); !errors.Is(err, ErrInvalidRecording) {
		t.Fatalf("NewImported of an uncounted MP3 = %v, want ErrInvalidRecording", err)
	}
}

// Fuzz the counter: no panic, and the count does not depend on how the bytes are split.
func FuzzMP3FrameCounter(f *testing.F) {
	f.Add(mp3Frames(cbr128, 3), uint8(1), int64(0))
	f.Add(bytes.Repeat(mixedFrames(low8, low16, mpeg2), 3), uint8(5), int64(3))
	f.Add([]byte("junk\xFF\xFB\x90\x64\xFF\xE3\x18"), uint8(2), int64(0))
	f.Fuzz(func(t *testing.T, data []byte, chunk uint8, from int64) {
		audio := ImportedAudio{Format: FormatMP3, counting: true, countFrom: from}
		whole, parts := NewMP3FrameCounter(audio), NewMP3FrameCounter(audio)
		_, _ = whole.Write(data)
		step := int(chunk) + 1
		for b := data; len(b) > 0; b = b[min(step, len(b)):] {
			_, _ = parts.Write(b[:min(step, len(b))])
		}
		if whole.samples != parts.samples || whole.confirmed != parts.confirmed {
			t.Fatalf("whole %d samples, in parts of %d %d", whole.samples, step, parts.samples)
		}
		if got, err := audio.Counted(whole); err == nil && (got.Duration <= 0 || got.Duration > MaxImportedDuration) {
			t.Fatalf("Counted = %+v", got)
		}
	})
}

func FuzzMP3Estimate(f *testing.F) {
	f.Add(xingFrame(1000))
	f.Add(vbriFrame(0))
	f.Add(mp3Frames(low8, 10))
	f.Fuzz(func(t *testing.T, b []byte) {
		if d, ok := mp3Estimate(b); ok && d < 0 {
			t.Fatalf("mp3Estimate = %v", d)
		}
	})
}
