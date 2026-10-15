package audiocapture

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

type piece struct {
	f      Format
	freq   float64
	frames int
}

// writeChunks writes each piece as its own chunk through the chunk writer.
func writeChunks(t *testing.T, store *dirStore, dir shared.ArtifactRef, pieces ...piece) {
	t.Helper()
	w, _ := newTestWriter(t, store, dir)
	for _, p := range pieces {
		if p.f == w.Format() { // a new writer closes the chunk, as a restart would
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w, _ = newTestWriter(t, store, dir)
		}
		if err := w.SetFormat(p.f); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(tone(p.f, p.freq, 0.5, 0, p.frames)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func readTrack(t *testing.T, store *dirStore, ref shared.ArtifactRef) (chunkInfo, []float64) {
	t.Helper()
	b := store.get(t, ref)
	info, err := readHeader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("the track is not a valid WAV: %v", err)
	}
	if info.Format != trackFormat() {
		t.Fatalf("track format = %s, want %s", info.Format, trackFormat())
	}
	return info, pcm16(b[pcmHeaderLen:])
}

// dominant checks that freq carries far more energy than its neighbours.
func dominant(t *testing.T, name string, samples []float64, freq float64) {
	t.Helper()
	at := goertzel(samples, freq, TrackRate)
	for _, other := range []float64{freq * 0.8, freq * 1.25, freq * 2} {
		if o := goertzel(samples, other, TrackRate); o*100 > at {
			t.Errorf("%s: power at %.0f Hz = %g, at %.0f Hz = %g; the tone moved", name, freq, at, other, o)
		}
	}
}

func TestFinalizeMixedFormats(t *testing.T) {
	store := newDirStore(t)
	dir, track := mustRef(t, "r1"), mustRef(t, "r1.wav")
	s16 := Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}
	f32 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	up := Format{SampleRate: 8000, Channels: 1, BitsPerSample: 24}
	s32x6 := Format{SampleRate: 96000, Channels: 6, BitsPerSample: 32}
	same := Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}
	pieces := []piece{{s16, 1000, 88200}, {f32, 1000, 96001}, {up, 1000, 8000}, {s32x6, 1000, 96000}, {same, 1000, 16000}}
	writeChunks(t, store, dir, pieces...)

	d, err := assemble(context.Background(), store, dir, track, quiet())
	if err != nil {
		t.Fatal(err)
	}

	info, samples := readTrack(t, store, track)
	var exact float64
	var want int64
	for _, p := range pieces {
		exact += float64(p.frames) / float64(p.f.SampleRate)
		want += outputFrames(int64(p.frames), p.f.SampleRate, TrackRate)
	}
	if info.Frames != want || int64(len(samples)) != want {
		t.Fatalf("track frames = %d (%d samples), want %d", info.Frames, len(samples), want)
	}
	if d != durationOf(want, TrackRate) {
		t.Fatalf("duration = %v, want frames / rate = %v", d, durationOf(want, TrackRate))
	}
	// Within one output sample per chunk of the audio time of the chunks.
	if diff := math.Abs(d.Seconds() - exact); diff > float64(len(pieces))/TrackRate {
		t.Fatalf("duration %v differs from the chunks' %v s by %v s", d, exact, diff)
	}

	// Each part keeps the 1 kHz tone at its level (amplitude 0.5: RMS 0.354), away from the filter edges.
	off := 0
	for _, p := range pieces {
		n := int(outputFrames(int64(p.frames), p.f.SampleRate, TrackRate))
		part := samples[off+200 : off+n-200]
		dominant(t, p.f.String(), part, 1000)
		if r := rms(part); math.Abs(r-0.5/math.Sqrt2) > 0.02 {
			t.Errorf("%s: RMS %.3f, want about 0.354", p.f, r)
		}
		off += n
	}
}

// The low-pass filter removes what 16 kHz cannot carry instead of folding it into the speech band.
func TestFinalizeFiltersAliases(t *testing.T) {
	store := newDirStore(t)
	dir, track := mustRef(t, "r1"), mustRef(t, "r1.wav")
	f32 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	writeChunks(t, store, dir, piece{f32, 10000, 48000})

	if _, err := assemble(context.Background(), store, dir, track, quiet()); err != nil {
		t.Fatal(err)
	}
	_, samples := readTrack(t, store, track)
	if r := rms(samples); r > 0.005 {
		t.Fatalf("a 10 kHz tone left RMS %.4f after resampling to 16 kHz", r)
	}
}

func TestFinalizeSkipsBrokenChunks(t *testing.T) {
	store := newDirStore(t)
	dir, track := mustRef(t, "r1"), mustRef(t, "r1.wav")
	f := Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}
	writeChunks(t, store, dir, piece{f, 1000, 16000})

	garbage := []byte("not audio at all, but long enough to look like a file")
	truncated := wavFile(fmtBody(f, false), nil, tone(f, 1000, 0.5, 0, 800))
	truncated = truncated[:len(truncated)-10]
	partial := append(encodeHeader(f, 0, false), tone(f, 1000, 0.5, 0, 800)...)
	store.put(t, mustRef(t, "r1/chunk-000002.wav"), garbage)
	store.put(t, mustRef(t, "r1/chunk-000003.wav"), truncated)
	store.put(t, mustRef(t, "r1/chunk-000004.wav.partial"), partial)
	store.put(t, mustRef(t, "r1/notes.txt"), []byte("ignored"))
	before := store.names(t, dir)

	d, err := assemble(context.Background(), store, dir, track, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if d != time.Second {
		t.Fatalf("duration = %v, want only the readable chunk (1s)", d)
	}
	if got := store.names(t, dir); !slices.Equal(got, before) {
		t.Fatalf("chunks after Finalize = %v, want them all left on disk %v", got, before)
	}
}

func TestFinalizeWithoutAudio(t *testing.T) {
	f := Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}
	tests := []struct {
		name  string
		setup func(t *testing.T, s *dirStore)
	}{
		{name: "missing directory", setup: func(*testing.T, *dirStore) {}},
		{name: "empty directory", setup: func(t *testing.T, s *dirStore) {
			if err := os.MkdirAll(s.path(mustRef(t, "r1")), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "only broken chunks", setup: func(t *testing.T, s *dirStore) {
			s.put(t, mustRef(t, "r1/chunk-000001.wav"), []byte("garbage"))
			s.put(t, mustRef(t, "r1/chunk-000002.wav.partial"), append(encodeHeader(f, 0, false), 1, 2))
		}},
		{name: "only empty chunks", setup: func(t *testing.T, s *dirStore) {
			s.put(t, mustRef(t, "r1/chunk-000001.wav"), encodeHeader(f, 0, true))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newDirStore(t)
			tt.setup(t, store)
			track := mustRef(t, "r1.wav")
			if _, err := assemble(context.Background(), store, mustRef(t, "r1"), track, quiet()); !errors.Is(err, recording.ErrNoAudio) {
				t.Fatalf("Finalize = %v, want ErrNoAudio", err)
			}
			if _, err := store.Size(context.Background(), track); !errors.Is(err, shared.ErrNotFound) {
				t.Fatal("a track was written without audio")
			}
		})
	}
}

func TestFinalizeTransientErrors(t *testing.T) {
	f := Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}
	busy := errors.New("file in use")
	tests := []struct {
		name   string
		inject func(s *dirStore)
		ctx    func() context.Context
	}{
		{name: "list fails", inject: func(s *dirStore) { s.listErr = busy }},
		{name: "open fails", inject: func(s *dirStore) { s.openErr = busy }},
		{name: "size fails", inject: func(s *dirStore) { s.sizeErr = busy }},
		{name: "cancelled", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newDirStore(t)
			writeChunks(t, store, mustRef(t, "r1"), piece{f, 1000, 1600})
			if tt.inject != nil {
				tt.inject(store)
			}
			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx()
			}
			_, err := assemble(ctx, store, mustRef(t, "r1"), mustRef(t, "r1.wav"), quiet())
			if err == nil || errors.Is(err, recording.ErrNoAudio) {
				t.Fatalf("Finalize = %v, want a transient error (not ErrNoAudio)", err)
			}
		})
	}
}

// A chunk that changes between the two passes fails the assembly instead of producing a wrong length.
func TestFinalizeChunkChangedMeanwhile(t *testing.T) {
	f := Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}
	store := newDirStore(t)
	dir := mustRef(t, "r1")
	writeChunks(t, store, dir, piece{f, 1000, 1600})
	changing := &changingStore{dirStore: store, after: 1, replace: func() {
		store.put(t, mustRef(t, "r1/chunk-000001.wav"), wavFile(fmtBody(f, false), nil, tone(f, 1000, 0.5, 0, 1601)))
	}}
	if _, err := assemble(context.Background(), changing, dir, mustRef(t, "r1.wav"), quiet()); err == nil || errors.Is(err, recording.ErrNoAudio) {
		t.Fatalf("Finalize = %v, want a transient error", err)
	}
}

type changingStore struct {
	*dirStore
	opens   int
	after   int
	replace func()
}

func (c *changingStore) Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	if c.opens == c.after {
		c.replace()
	}
	c.opens++
	return c.dirStore.Open(ctx, ref)
}

func TestFinalizeIsIdempotent(t *testing.T) {
	store := newDirStore(t)
	dir, track := mustRef(t, "r1"), mustRef(t, "r1.wav")
	writeChunks(t, store, dir, piece{Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}, 1000, 44100})

	d1, err := assemble(context.Background(), store, dir, track, quiet())
	if err != nil {
		t.Fatal(err)
	}
	first := store.get(t, track)
	d2, err := assemble(context.Background(), store, dir, track, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 || !bytes.Equal(first, store.get(t, track)) {
		t.Fatal("a second Finalize produced another track")
	}
}

// Consecutive chunks of one format are one stream: the joint has no click from the filter edges.
func TestFinalizeJoinsSameFormatChunksSmoothly(t *testing.T) {
	store := newDirStore(t)
	dir, track := mustRef(t, "r1"), mustRef(t, "r1.wav")
	f := Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	w, _ := newTestWriter(t, store, dir)
	if err := w.SetFormat(f); err != nil {
		t.Fatal(err)
	}
	// 31 s of a continuous tone: one full 30 s chunk and the next one.
	if err := w.Write(tone(f, 1000, 0.5, 0, 31*48000)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := assemble(context.Background(), store, dir, track, quiet()); err != nil {
		t.Fatal(err)
	}

	_, samples := readTrack(t, store, track)
	joint := 30 * TrackRate
	for i := joint - 50; i < joint+50; i++ {
		want := 0.5 * math.Sin(2*math.Pi*1000*float64(i)/TrackRate)
		if math.Abs(samples[i]-want) > 0.01 {
			t.Fatalf("sample %d at the chunk joint = %.4f, want %.4f", i, samples[i], want)
		}
	}
}

func TestResamplerTimeline(t *testing.T) {
	for _, rate := range []int{8000, 11025, 22050, 44100, 48000, 96000, 192000, 44101} {
		r := newResampler(rate, TrackRate)
		in := make([]float32, rate)
		var out []float32
		for i := 0; i < len(in); i += 1000 {
			out = r.push(in[i:min(i+1000, len(in))], out)
		}
		want := outputFrames(int64(rate), uint32(rate), TrackRate)
		out = r.flush(want-int64(len(out)), out)
		if int64(len(out)) != want || want != TrackRate {
			t.Errorf("%d Hz: %d outputs, want %d", rate, len(out), TrackRate)
		}
	}
}
