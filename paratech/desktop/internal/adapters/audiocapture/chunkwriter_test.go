package audiocapture

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

var testFormat = Format{SampleRate: 8000, Channels: 1, BitsPerSample: 16}

type closedChunk struct {
	n      int
	frames int64
	format Format
}

func newTestWriter(t *testing.T, store *dirStore, dir shared.ArtifactRef) (*chunkWriter, *[]closedChunk) {
	t.Helper()
	var closed []closedChunk
	w, err := newChunkWriter(context.Background(), store, dir, 30, func(n int, frames int64, f Format) {
		closed = append(closed, closedChunk{n, frames, f})
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, &closed
}

// abandon simulates a kill: buffered bytes reach the file, the header stays a placeholder and the name temporary.
func (w *chunkWriter) abandon() {
	if w.cur != nil {
		_ = w.buf.Flush()
		_ = w.cur.Close()
		w.cur = nil
	}
	w.closed = true
}

func checkChunk1(t *testing.T, store *dirStore, ref shared.ArtifactRef) chunkInfo {
	t.Helper()
	b := store.get(t, ref)
	info, err := readHeader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("%s: %v", baseName(ref), err)
	}
	return info
}

func TestChunkWriterRotatesByFrames(t *testing.T) {
	store := newDirStore(t)
	dir := mustRef(t, "r1")
	w, closed := newTestWriter(t, store, dir)
	if err := w.SetFormat(testFormat); err != nil {
		t.Fatal(err)
	}

	// 2.5 chunks of 30 s in uneven pieces.
	audio := tone(testFormat, 440, 0.5, 0, 600000)
	for off := 0; off < len(audio); {
		n := min(len(audio)-off, 2*7777)
		if err := w.Write(audio[off : off+n]); err != nil {
			t.Fatal(err)
		}
		off += n
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	want := []closedChunk{{1, 240000, testFormat}, {2, 240000, testFormat}, {3, 120000, testFormat}}
	if !slices.Equal(*closed, want) {
		t.Fatalf("closed chunks = %+v, want %+v", *closed, want)
	}
	if got := store.names(t, dir); !slices.Equal(got, []string{"chunk-000001.wav", "chunk-000002.wav", "chunk-000003.wav"}) {
		t.Fatalf("files = %v", got)
	}

	// The patched headers describe the files, and the data is the stream in order.
	var joined []byte
	for i, c := range want {
		ref := mustRef(t, "r1/"+[]string{"chunk-000001.wav", "chunk-000002.wav", "chunk-000003.wav"}[i])
		if info := checkChunk1(t, store, ref); info.Frames != c.frames || info.Format != testFormat {
			t.Fatalf("chunk %d header = %+v", c.n, info)
		}
		joined = append(joined, store.get(t, ref)[pcmHeaderLen:]...)
	}
	if !bytes.Equal(joined, audio) {
		t.Fatal("the chunks do not hold the written audio in order")
	}
}

func TestChunkWriterAfterAKillContinuesNumbering(t *testing.T) {
	store := newDirStore(t)
	dir := mustRef(t, "r1")
	w, _ := newTestWriter(t, store, dir)
	if err := w.SetFormat(testFormat); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(tone(testFormat, 440, 0.5, 0, 300000)); err != nil {
		t.Fatal(err)
	}
	w.abandon()

	first := store.get(t, mustRef(t, "r1/chunk-000001.wav"))
	partial := mustRef(t, "r1/chunk-000002.wav.partial")
	b := store.get(t, partial)
	if _, err := readHeader(bytes.NewReader(b), int64(len(b))); !errors.Is(err, errNotAudio) {
		t.Fatalf("the chunk of a killed writer reads as %v, want errNotAudio", err)
	}

	again, closed := newTestWriter(t, store, dir)
	if err := again.SetFormat(testFormat); err != nil {
		t.Fatal(err)
	}
	if err := again.Write(tone(testFormat, 440, 0.5, 0, 100)); err != nil {
		t.Fatal(err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}

	if len(*closed) != 1 || (*closed)[0].n != 3 {
		t.Fatalf("restart closed %+v, want chunk 3", *closed)
	}
	want := []string{"chunk-000001.wav", "chunk-000002.wav.partial", "chunk-000003.wav"}
	if got := store.names(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if !bytes.Equal(store.get(t, mustRef(t, "r1/chunk-000001.wav")), first) || !bytes.Equal(store.get(t, partial), b) {
		t.Fatal("the restart changed earlier chunks")
	}
}

func TestChunkWriterFormatChangeClosesTheChunk(t *testing.T) {
	store := newDirStore(t)
	dir := mustRef(t, "r1")
	w, closed := newTestWriter(t, store, dir)
	f2 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}

	steps := []struct {
		f      Format
		frames int
	}{{testFormat, 1000}, {testFormat, 500}, {f2, 4800}, {testFormat, 10}}
	for _, s := range steps {
		if err := w.SetFormat(s.f); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(tone(s.f, 440, 0.5, 0, s.frames)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	want := []closedChunk{{1, 1500, testFormat}, {2, 4800, f2}, {3, 10, testFormat}}
	if !slices.Equal(*closed, want) {
		t.Fatalf("closed = %+v, want %+v", *closed, want)
	}
	if info := checkChunk1(t, store, mustRef(t, "r1/chunk-000002.wav")); info.Format != f2 || info.Frames != 4800 {
		t.Fatalf("float chunk = %+v", info)
	}
}

func TestChunkWriterSilenceAndPadding(t *testing.T) {
	store := newDirStore(t)
	w, _ := newTestWriter(t, store, mustRef(t, "r1"))
	s24 := Format{SampleRate: 8000, Channels: 1, BitsPerSample: 24}
	if err := w.SetFormat(s24); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSilence(20001); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	ref := mustRef(t, "r1/chunk-000001.wav")
	b := store.get(t, ref)
	if len(b)%2 != 0 {
		t.Fatalf("file of %d bytes: the odd data chunk lacks its pad byte", len(b))
	}
	if info := checkChunk1(t, store, ref); info.Frames != 20001 {
		t.Fatalf("frames = %d", info.Frames)
	}
	if bytes.ContainsFunc(b[pcmHeaderLen:], func(r rune) bool { return r != 0 }) {
		t.Fatal("silence is not all zeros")
	}
}

func TestChunkWriterErrors(t *testing.T) {
	store := newDirStore(t)
	dir := mustRef(t, "r1")
	w, _ := newTestWriter(t, store, dir)

	if err := w.Write([]byte{0, 0}); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("Write without a format = %v", err)
	}
	if err := w.SetFormat(Format{SampleRate: 8000, Channels: 1, BitsPerSample: 8}); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("SetFormat 8-bit = %v", err)
	}
	if err := w.SetFormat(testFormat); err != nil {
		t.Fatal(err)
	}
	if err := w.Write([]byte{1, 2, 3}); !errors.Is(err, ErrPartialFrame) {
		t.Fatalf("Write of a partial frame = %v", err)
	}

	// Another writer took the next name meanwhile: creation is exclusive, nothing is overwritten.
	planted := []byte("someone else's file")
	store.put(t, mustRef(t, "r1/chunk-000001.wav.partial"), planted)
	if err := w.Write(tone(testFormat, 440, 0.5, 0, 10)); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Write onto an existing chunk = %v, want ErrConflict", err)
	}
	if err := w.Write(tone(testFormat, 440, 0.5, 0, 10)); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("the failure is not sticky: %v", err)
	}
	if !bytes.Equal(store.get(t, mustRef(t, "r1/chunk-000001.wav.partial")), planted) {
		t.Fatal("the existing file was changed")
	}
	if err := w.Close(); err == nil {
		t.Fatal("Close after a failed write must report it")
	}

	store.listErr = errors.New("disk gone")
	if _, err := newChunkWriter(context.Background(), store, dir, 30, nil); err == nil {
		t.Fatal("a failing scan must fail the writer")
	}
}

func TestChunkNumber(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		partial bool
		ok      bool
	}{
		{"chunk-000001.wav", 1, false, true},
		{"chunk-123456.wav.partial", 123456, true, true},
		{"chunk-00001.wav", 0, false, false},
		{"chunk-0000001.wav", 0, false, false},
		{"chunk-00000a.wav", 0, false, false},
		{"chunk-000001.WAV", 0, false, false},
		{"chunk-000001.wav.tmp", 0, false, false},
		{"other.wav", 0, false, false},
	}
	for _, tt := range tests {
		n, partial, ok := chunkNumber(tt.name)
		if n != tt.n || partial != tt.partial || ok != tt.ok {
			t.Errorf("chunkNumber(%q) = %d, %v, %v", tt.name, n, partial, ok)
		}
	}
}
