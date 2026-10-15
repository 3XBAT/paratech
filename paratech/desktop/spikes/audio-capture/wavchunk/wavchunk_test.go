package wavchunk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var (
	pcm16 = Format{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	f32   = Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}

	// slow has 1000 frames per second, so a 10 ms chunk holds 10 frames.
	slow = Format{SampleRate: 1000, Channels: 1, BitsPerSample: 16}
)

func frames(f Format, n int) []byte {
	b := make([]byte, n*f.BlockAlign())
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func newWriter(t *testing.T, dir string, f Format, d time.Duration, onChunk func(Chunk)) *Writer {
	t.Helper()
	w, err := New(Config{Dir: dir, ChunkDuration: d, OnChunk: onChunk}, f)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func validate(t *testing.T, dir string) []Result {
	t.Helper()
	res, err := ValidateDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestFormatValidate(t *testing.T) {
	tests := []struct {
		name string
		f    Format
		ok   bool
	}{
		{"pcm16 stereo", pcm16, true},
		{"float32 stereo", f32, true},
		{"pcm24 mono", Format{SampleRate: 44100, Channels: 1, BitsPerSample: 24}, true},
		{"float64", Format{SampleRate: 16000, Channels: 1, BitsPerSample: 64, Float: true}, true},
		{"zero rate", Format{Channels: 2, BitsPerSample: 16}, false},
		{"rate too high", Format{SampleRate: 1_000_000, Channels: 2, BitsPerSample: 16}, false},
		{"no channels", Format{SampleRate: 48000, BitsPerSample: 16}, false},
		{"too many channels", Format{SampleRate: 48000, Channels: 33, BitsPerSample: 16}, false},
		{"float16", Format{SampleRate: 48000, Channels: 2, BitsPerSample: 16, Float: true}, false},
		{"pcm12", Format{SampleRate: 48000, Channels: 2, BitsPerSample: 12}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.f.Validate()
			if (err == nil) != tt.ok {
				t.Fatalf("Validate() = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("error %v does not wrap ErrInvalidFormat", err)
			}
		})
	}
}

func TestHeaderFields(t *testing.T) {
	le := binary.LittleEndian
	tests := []struct {
		name      string
		f         Format
		frames    int
		headerLen int
		tag       uint16
		fmtLen    uint32
	}{
		{name: "pcm16 stereo", f: pcm16, frames: 480, headerLen: 44, tag: tagPCM, fmtLen: 16},
		{name: "float32 stereo", f: f32, frames: 480, headerLen: 58, tag: tagFloat, fmtLen: 18},
		{name: "pcm16 mono one frame", f: Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}, frames: 1, headerLen: 44, tag: tagPCM, fmtLen: 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			w := newWriter(t, dir, tt.f, time.Second, nil)
			if err := w.Write(frames(tt.f, tt.frames)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			b, err := os.ReadFile(filepath.Join(dir, "chunk-000001.wav"))
			if err != nil {
				t.Fatal(err)
			}
			data := tt.frames * tt.f.BlockAlign()
			if len(b) != tt.headerLen+data {
				t.Fatalf("file is %d bytes, want %d", len(b), tt.headerLen+data)
			}

			checks := []struct {
				what      string
				got, want uint64
			}{
				{"RIFF size", uint64(le.Uint32(b[4:8])), uint64(len(b) - 8)},
				{"fmt length", uint64(le.Uint32(b[16:20])), uint64(tt.fmtLen)},
				{"format tag", uint64(le.Uint16(b[20:22])), uint64(tt.tag)},
				{"channels", uint64(le.Uint16(b[22:24])), uint64(tt.f.Channels)},
				{"sample rate", uint64(le.Uint32(b[24:28])), uint64(tt.f.SampleRate)},
				{"byte rate", uint64(le.Uint32(b[28:32])), uint64(tt.f.ByteRate())},
				{"block align", uint64(le.Uint16(b[32:34])), uint64(tt.f.BlockAlign())},
				{"bits", uint64(le.Uint16(b[34:36])), uint64(tt.f.BitsPerSample)},
				{"data size", uint64(le.Uint32(b[tt.headerLen-4 : tt.headerLen])), uint64(data)},
			}
			if tt.f.Float {
				checks = append(checks, struct {
					what      string
					got, want uint64
				}{"fact sample length", uint64(le.Uint32(b[46:50])), uint64(tt.frames)})
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s = %d, want %d", c.what, c.got, c.want)
				}
			}
			if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[tt.headerLen-8:tt.headerLen-4]) != "data" {
				t.Errorf("bad chunk ids in % x", b[:tt.headerLen])
			}
			if !bytes.Equal(b[tt.headerLen:], frames(tt.f, tt.frames)) {
				t.Error("payload differs from the written frames")
			}

			info, err := Read(bytes.NewReader(b), int64(len(b)))
			if err != nil {
				t.Fatal(err)
			}
			if info.Format != tt.f || info.Frames != int64(tt.frames) {
				t.Fatalf("Read = %+v, want %v with %d frames", info, tt.f, tt.frames)
			}
		})
	}
}

func TestRotation(t *testing.T) {
	tests := []struct {
		name   string
		writes []int // frames per Write call
		want   []int64
	}{
		{name: "one big write", writes: []int{25}, want: []int64{10, 10, 5}},
		{name: "small writes", writes: []int{3, 7, 15}, want: []int64{10, 10, 5}},
		{name: "exact multiple", writes: []int{10, 10}, want: []int64{10, 10}},
		{name: "one frame", writes: []int{1}, want: []int64{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			var closed []Chunk
			w := newWriter(t, dir, slow, 10*time.Millisecond, func(c Chunk) { closed = append(closed, c) })
			if w.FramesPerChunk() != 10 {
				t.Fatalf("FramesPerChunk = %d, want 10", w.FramesPerChunk())
			}
			for _, n := range tt.writes {
				if err := w.Write(frames(slow, n)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			res := validate(t, dir)
			var got []int64
			for i, r := range res {
				if r.Err != nil {
					t.Fatalf("%s: %v", r.Name, r.Err)
				}
				if want := "chunk-00000" + string(rune('1'+i)) + ".wav"; r.Name != want {
					t.Fatalf("chunk %d is %s, want %s", i, r.Name, want)
				}
				got = append(got, r.Info.Frames)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("chunk frames = %v, want %v", got, tt.want)
			}
			if len(closed) != len(tt.want) || closed[0].Duration() != slow.Duration(tt.want[0]) {
				t.Fatalf("OnChunk calls = %+v", closed)
			}
		})
	}
}

func TestKilledLastChunk(t *testing.T) {
	tests := []struct {
		name    string
		frames  int
		valid   []string
		invalid []string
	}{
		{name: "mid chunk", frames: 25, valid: []string{"chunk-000001.wav", "chunk-000002.wav"}, invalid: []string{"chunk-000003.wav.partial"}},
		{name: "killed at a chunk boundary", frames: 20, valid: []string{"chunk-000001.wav", "chunk-000002.wav"}},
		{name: "first chunk", frames: 4, invalid: []string{"chunk-000001.wav.partial"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			w := newWriter(t, dir, slow, 10*time.Millisecond, nil)
			if err := w.Write(frames(slow, tt.frames)); err != nil {
				t.Fatal(err)
			}
			w.abandon()

			var valid, invalid []string
			for _, r := range validate(t, dir) {
				if r.Err == nil {
					valid = append(valid, r.Name)
					continue
				}
				if !errors.Is(r.Err, ErrIncomplete) || !r.Partial() {
					t.Errorf("%s: %v (partial=%v), want ErrIncomplete on a partial file", r.Name, r.Err, r.Partial())
				}
				invalid = append(invalid, r.Name)
			}
			if !slices.Equal(valid, tt.valid) || !slices.Equal(invalid, tt.invalid) {
				t.Fatalf("valid %v invalid %v, want %v and %v", valid, invalid, tt.valid, tt.invalid)
			}
		})
	}
}

func TestReadDetectsDamage(t *testing.T) {
	dir := t.TempDir()
	w := newWriter(t, dir, pcm16, time.Second, nil)
	if err := w.Write(frames(pcm16, 100)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(filepath.Join(dir, "chunk-000001.wav"))
	if err != nil {
		t.Fatal(err)
	}

	patched := func(off int, v uint32) []byte {
		b := slices.Clone(good)
		binary.LittleEndian.PutUint32(b[off:], v)
		return b
	}
	tests := []struct {
		name string
		b    []byte
		want error
	}{
		{name: "truncated", b: good[:len(good)-7], want: ErrIncomplete},
		{name: "header only", b: good[:44], want: ErrIncomplete},
		{name: "tiny", b: good[:5], want: ErrIncomplete},
		{name: "placeholder header", b: append(encodeHeader(pcm16, 0, false), good[44:]...), want: ErrIncomplete},
		{name: "data size past end", b: patched(40, 1000), want: ErrIncomplete},
		{name: "not riff", b: append([]byte("RIFX"), good[4:]...), want: ErrNotWAV},
		{name: "unknown tag", b: func() []byte {
			b := slices.Clone(good)
			binary.LittleEndian.PutUint16(b[20:], 0x55)
			return b
		}(), want: ErrNotWAV},
		{name: "partial frame", b: func() []byte {
			b := patched(40, 399)
			binary.LittleEndian.PutUint32(b[4:], uint32(36+399))
			return b[:44+399]
		}(), want: ErrIncomplete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Read(bytes.NewReader(tt.b), int64(len(tt.b))); !errors.Is(err, tt.want) {
				t.Fatalf("Read = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCloseWithoutFrames(t *testing.T) {
	dir := t.TempDir()
	w := newWriter(t, dir, f32, time.Second, nil)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if res := validate(t, dir); len(res) != 0 {
		t.Fatalf("expected no chunk, got %+v", res)
	}
	if err := w.Write(frames(f32, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after Close = %v, want ErrClosed", err)
	}
}

func TestWriteRejectsPartialFrame(t *testing.T) {
	w := newWriter(t, t.TempDir(), pcm16, time.Second, nil)
	defer func() { _ = w.Close() }()
	if err := w.Write(make([]byte, 6)); !errors.Is(err, ErrPartialFrame) {
		t.Fatalf("Write = %v, want ErrPartialFrame", err)
	}
}

func TestNumberingContinuesAfterExistingChunks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"chunk-000007.wav", "chunk-000009.wav.partial", "notes.txt", "chunk-1.wav"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w := newWriter(t, dir, slow, 10*time.Millisecond, nil)
	if err := w.Write(frames(slow, 3)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "chunk-000010.wav")); err != nil {
		t.Fatalf("expected chunk-000010.wav: %v", err)
	}
}

func TestSetFormatRotates(t *testing.T) {
	dir := t.TempDir()
	w := newWriter(t, dir, pcm16, time.Second, nil)
	steps := []struct {
		f      Format
		frames int
	}{{pcm16, 100}, {pcm16, 50}, {f32, 30}}
	for _, s := range steps {
		if err := w.SetFormat(s.f); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(frames(s.f, s.frames)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	res := validate(t, dir)
	if len(res) != 2 || res[0].Info.Format != pcm16 || res[0].Info.Frames != 150 || res[1].Info.Format != f32 || res[1].Info.Frames != 30 {
		t.Fatalf("chunks = %+v", res)
	}
	if err := w.SetFormat(Format{}); err == nil {
		t.Fatal("SetFormat on a closed writer must fail")
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		f    Format
		want error
	}{
		{name: "missing dir", cfg: Config{Dir: filepath.Join(t.TempDir(), "nope"), ChunkDuration: time.Second}, f: pcm16},
		{name: "zero duration", cfg: Config{Dir: t.TempDir()}, f: pcm16},
		{name: "bad format", cfg: Config{Dir: t.TempDir(), ChunkDuration: time.Second}, f: Format{}, want: ErrInvalidFormat},
		{name: "too long for WAV", cfg: Config{Dir: t.TempDir(), ChunkDuration: 10 * time.Hour}, f: Format{SampleRate: 384000, Channels: 32, BitsPerSample: 64, Float: true}, want: ErrChunkTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := New(tt.cfg, tt.f)
			if err == nil {
				_ = w.Close()
				t.Fatal("New succeeded")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("New = %v, want %v", err, tt.want)
			}
		})
	}
}
