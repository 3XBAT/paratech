package audiocapture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var s16 = testenv.FakeRecorderFormat() // 8 kHz mono 16-bit

func ref(t *testing.T, p string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.NewArtifactRef(shared.ArtifactRecordings, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// syncBuffer collects the child's stdout.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// events decodes every line written so far.
func (b *syncBuffer) events(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("child wrote a non-JSON line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func (b *syncBuffer) find(t *testing.T, typ string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ev := range b.events(t) {
		if ev["type"] == typ {
			out = append(out, ev)
		}
	}
	return out
}

type childRun struct {
	stdin *io.PipeWriter
	out   *syncBuffer
	code  chan int
	store *audiocapture.TestStore
}

// startChild runs RunChild in this process with src and sends req (or the raw line when req is a string).
func startChild(t *testing.T, ctx context.Context, src audiocapture.Source, req any, cfg audiocapture.ChildConfig) *childRun {
	t.Helper()
	var line []byte
	switch r := req.(type) {
	case string:
		line = []byte(r + "\n")
	case audiocapture.Request:
		var err error
		if line, err = audiocapture.EncodeRequest(r); err != nil {
			t.Fatal(err)
		}
	}

	pr, pw := io.Pipe()
	run := &childRun{stdin: pw, out: &syncBuffer{}, code: make(chan int, 1), store: audiocapture.NewTestStore(t.TempDir())}
	go func() {
		run.code <- audiocapture.RunChild(ctx, pr, run.out, src, run.store, nil, cfg)
	}()
	t.Cleanup(func() { _ = pw.Close() }) // ends the stdin watcher
	go func() { _, _ = pw.Write(line) }()
	return run
}

func (c *childRun) exit(t *testing.T) int {
	t.Helper()
	select {
	case code := <-c.code:
		return code
	case <-time.After(20 * time.Second):
		t.Fatal("the child did not exit")
		return -1
	}
}

// waitFrames waits until the heartbeat reports frames written.
func (c *childRun) waitFrames(t *testing.T, frames int) {
	t.Helper()
	testenv.Eventually(t, "the packets are written", func() bool {
		stats := c.out.find(t, "stats")
		return len(stats) > 0 && stats[len(stats)-1]["frames"] == float64(frames)
	}, nil)
}

func request(t *testing.T) audiocapture.Request {
	return audiocapture.Request{
		Recording: "rec1", Chunks: ref(t, "rec1"), Target: audiocapture.Target{Mode: audiocapture.ModeExclude, PID: 99},
		ChunkSeconds: 30,
	}
}

// chunk reads a final chunk and returns its frames and samples.
func chunk(t *testing.T, store *audiocapture.TestStore, name string) (audiocapture.Format, int64, []byte) {
	t.Helper()
	f, frames, data, err := audiocapture.ReadWAV(store.Get(t, ref(t, "rec1/"+name)))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f, frames, data
}

func TestChildStopsWhenStdinCloses(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	src := &testenv.ToneSource{Format: s16, PacketFrames: 800, Limit: 450, Drain: 2}
	run := startChild(t, context.Background(), src, request(t), audiocapture.ChildConfig{StatsInterval: 10 * time.Millisecond})

	testenv.Eventually(t, "every packet up to the limit is written", func() bool {
		stats := run.out.find(t, "stats")
		return len(stats) > 0 && stats[len(stats)-1]["frames"] == float64(450*800)
	}, nil)
	if len(run.out.find(t, "chunk")) != 1 {
		t.Fatal("the first chunk did not close at 30 s")
	}
	_ = run.stdin.Close()
	if code := run.exit(t); code != audiocapture.ChildExitOK {
		t.Fatalf("exit code %d", code)
	}

	// 450 packets of 100 ms plus 2 drained ones: a full 30 s chunk and the rest, closed on the stop.
	if got := run.store.Names(t, ref(t, "rec1")); !slices.Equal(got, []string{"chunk-000001.wav", "chunk-000002.wav"}) {
		t.Fatalf("files = %v", got)
	}
	if _, frames, _ := chunk(t, run.store, "chunk-000001.wav"); frames != 240000 {
		t.Fatalf("first chunk frames = %d", frames)
	}
	if _, frames, _ := chunk(t, run.store, "chunk-000002.wav"); frames != 452*800-240000 {
		t.Fatalf("last chunk frames = %d", frames)
	}

	started := run.out.find(t, "started")
	stopped := run.out.find(t, "stopped")
	if len(started) != 1 || started[0]["mode"] != "exclude" || started[0]["build"] != "10.0.19045" {
		t.Fatalf("started = %v", started)
	}
	if len(stopped) != 1 || stopped[0]["frames"] != float64(452*800) {
		t.Fatalf("stopped = %v", stopped)
	}
	if src.Closes() != 1 {
		t.Fatalf("stream closed %d times", src.Closes())
	}
}

func TestChildReportsAFailedCapture(t *testing.T) {
	src := &testenv.ToneSource{Format: s16, PacketFrames: 800, FailAfter: 100}
	run := startChild(t, context.Background(), src, request(t), audiocapture.ChildConfig{})
	if code := run.exit(t); code != audiocapture.ChildExitFailed {
		t.Fatalf("exit code %d, want %d", code, audiocapture.ChildExitFailed)
	}
	failed := run.out.find(t, "failed")
	if len(failed) != 1 || !strings.Contains(failed[0]["reason"].(string), "device lost") {
		t.Fatalf("failed = %v", failed)
	}
	// What was captured is closed properly.
	if _, frames, _ := chunk(t, run.store, "chunk-000001.wav"); frames != 80000 {
		t.Fatalf("frames = %d", frames)
	}
	if len(run.out.find(t, "stopped")) != 0 {
		t.Fatal("a failed capture must not report a stop")
	}
}

func TestChildWritesTheLeadSilenceOfARestart(t *testing.T) {
	src := &testenv.ToneSource{Format: s16, PacketFrames: 800, Limit: 10}
	req := request(t)
	req.LeadSilence = 1500 * time.Millisecond
	run := startChild(t, context.Background(), src, req, audiocapture.ChildConfig{StatsInterval: 10 * time.Millisecond})
	run.waitFrames(t, 12000+8000)
	_ = run.stdin.Close()
	if code := run.exit(t); code != 0 {
		t.Fatalf("exit code %d", code)
	}

	_, frames, data := chunk(t, run.store, "chunk-000001.wav")
	if frames != 12000+8000 {
		t.Fatalf("frames = %d, want 1.5 s of silence and 1 s of tone", frames)
	}
	if bytes.ContainsFunc(data[:24000], func(r rune) bool { return r != 0 }) {
		t.Fatal("the lead is not silence")
	}
	if !bytes.Equal(data[24000:], testenv.ToneFrames(s16, 1000, 0.5, 0, 8000)) {
		t.Fatal("the tone does not follow the lead")
	}
}

// fakeNow is a monotonic clock the test moves.
type fakeNow struct {
	base time.Time
	off  atomic.Int64
}

func (f *fakeNow) now() time.Time          { return f.base.Add(time.Duration(f.off.Load())) }
func (f *fakeNow) advance(d time.Duration) { f.off.Add(int64(d)) }

func TestChildFillsAGapWithSilence(t *testing.T) {
	clock := &fakeNow{base: time.Now()}
	src := &testenv.ToneSource{Format: s16, PacketFrames: 800, Limit: 5, DiscontinuityAt: 5, SilentAt: 3,
		OnPacket: func(n int) {
			if n == 5 {
				clock.advance(10 * time.Second) // the device was gone for a while
			}
		}}
	run := startChild(t, context.Background(), src, request(t), audiocapture.ChildConfig{Now: clock.now, StatsInterval: 10 * time.Millisecond})
	run.waitFrames(t, 5*800+76000)
	_ = run.stdin.Close()
	if code := run.exit(t); code != 0 {
		t.Fatalf("exit code %d", code)
	}

	// 0.4 s of audio, then the packet ending at 10 s: 9.5 s of silence before it.
	_, frames, data := chunk(t, run.store, "chunk-000001.wav")
	if frames != 5*800+76000 {
		t.Fatalf("frames = %d, want %d", frames, 5*800+76000)
	}
	silence := data[4*1600 : 4*1600+76000*2]
	if bytes.ContainsFunc(silence, func(r rune) bool { return r != 0 }) {
		t.Fatal("the gap is not silence")
	}
	if bytes.ContainsFunc(data[2*1600:3*1600], func(r rune) bool { return r != 0 }) {
		t.Fatal("a silent packet was not written as zeros")
	}
}

func TestChildFormatSwitchClosesTheChunk(t *testing.T) {
	f32 := audiocapture.Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	src := &testenv.ToneSource{Format: s16, PacketFrames: 800, Limit: 20, SwitchAfter: 10, SwitchFormat: f32,
		SwitchMode: audiocapture.ModeSystem}
	run := startChild(t, context.Background(), src, request(t), audiocapture.ChildConfig{StatsInterval: 10 * time.Millisecond})
	run.waitFrames(t, 10*800+10*480)
	_ = run.stdin.Close()
	if code := run.exit(t); code != 0 {
		t.Fatalf("exit code %d", code)
	}

	sw := run.out.find(t, "switch")[0]
	if sw["from"] != "exclude" || sw["to"] != "system" {
		t.Fatalf("switch = %v", sw)
	}
	if f, frames, _ := chunk(t, run.store, "chunk-000001.wav"); f != s16 || frames != 8000 {
		t.Fatalf("chunk 1 = %s, %d frames", f, frames)
	}
	if f, frames, _ := chunk(t, run.store, "chunk-000002.wav"); f != f32 || frames != 10*480 {
		t.Fatalf("chunk 2 = %s, %d frames", f, frames)
	}
}

func TestChildOpensSystemModeAfterAFallback(t *testing.T) {
	src := &testenv.ToneSource{Format: s16, Limit: 1}
	req := request(t)
	req.Floor = audiocapture.ModeSystem
	run := startChild(t, context.Background(), src, req, audiocapture.ChildConfig{})
	testenv.Eventually(t, "the capture starts", func() bool { return len(run.out.find(t, "started")) == 1 }, nil)
	_ = run.stdin.Close()
	run.exit(t)
	if got := src.Opened(); !slices.Equal(got, []audiocapture.Target{{Mode: audiocapture.ModeSystem}}) {
		t.Fatalf("opened %v, want system mode only", got)
	}
}

func TestChildStopsWhenTheContextEnds(t *testing.T) {
	src := &testenv.ToneSource{Format: s16, Limit: 3}
	ctx, cancel := context.WithCancel(context.Background())
	run := startChild(t, ctx, src, request(t), audiocapture.ChildConfig{})
	testenv.Eventually(t, "the capture starts", func() bool { return len(run.out.find(t, "started")) == 1 }, nil)
	cancel()
	if code := run.exit(t); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if len(run.out.find(t, "stopped")) != 1 {
		t.Fatal("no stopped event")
	}
}

// A stream that never ends after Stop is cut off after the drain timeout.
func TestChildCutsOffAStreamThatDoesNotDrain(t *testing.T) {
	src := &testenv.ToneSource{Format: s16, HangAfter: 3}
	run := startChild(t, context.Background(), src, request(t), audiocapture.ChildConfig{DrainTimeout: 50 * time.Millisecond})
	testenv.Eventually(t, "the capture starts", func() bool { return len(run.out.find(t, "started")) == 1 }, nil)
	_ = run.stdin.Close()
	if code := run.exit(t); code != 0 {
		t.Fatalf("exit code %d", code)
	}
}

func TestChildRefusesBadRequests(t *testing.T) {
	tests := []struct {
		name, line string
	}{
		{"not json", "start please"},
		{"unknown field", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"system","pid":0},"chunk_seconds":30,"lead_silence_ms":0,"cmd":"x"}`},
		{"oversized", `{"v":1,"pad":"` + strings.Repeat("x", 5000) + `"}`},
		{"empty stdin", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &testenv.ToneSource{Format: s16}
			run := startChild(t, context.Background(), src, tt.line, audiocapture.ChildConfig{})
			if tt.line == "" {
				_ = run.stdin.Close()
			}
			if code := run.exit(t); code != audiocapture.ChildExitBadRequest {
				t.Fatalf("exit code %d, want %d", code, audiocapture.ChildExitBadRequest)
			}
			failed := run.out.find(t, "failed")
			if len(failed) != 1 || !strings.HasPrefix(failed[0]["reason"].(string), "bad request") {
				t.Fatalf("failed = %v", failed)
			}
			if len(src.Opened()) != 0 {
				t.Fatal("a bad request opened the capture")
			}
		})
	}
}

func TestChildReportsAnUnavailableCapture(t *testing.T) {
	src := &testenv.ToneSource{Format: s16, OpenErr: audiocapture.ErrUnavailable}
	run := startChild(t, context.Background(), src, request(t), audiocapture.ChildConfig{})
	if code := run.exit(t); code != audiocapture.ChildExitFailed {
		t.Fatalf("exit code %d", code)
	}
	failed := run.out.find(t, "failed")
	if len(failed) != 1 || failed[0]["unavailable"] != true {
		t.Fatalf("failed = %v", failed)
	}
	if len(run.out.find(t, "attempt")) != 1 {
		t.Fatal("the failed attempt is not reported")
	}
}

func TestPlatformSourceIsUnavailableInThisBuild(t *testing.T) {
	if audiocapture.Supported() {
		t.Skip("this build captures; opening its source would touch the audio devices (see TestLiveCapture)")
	}
	_, err := audiocapture.PlatformSource().Open(context.Background(), audiocapture.Target{Mode: audiocapture.ModeSystem}, nil)
	if !errors.Is(err, audiocapture.ErrUnavailable) {
		t.Fatalf("Open = %v, want ErrUnavailable", err)
	}
}
