package whispercli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

// newVADEngine is the fixture engine with Config.VADModel set to vad; the log goes to buf.
func newVADEngine(t *testing.T, f *fixture, vad string, buf *bytes.Buffer) (*Engine, error) {
	t.Helper()
	eng, err := New(Config{
		Binary: f.eng.cfg.Binary, DataRoot: f.root, WorkDir: f.work, Threads: 2, Language: "ru", VADModel: vad,
		Log: slog.New(slog.NewTextHandler(buf, nil)),
	}, f.paths, f.store)
	if err == nil {
		eng.env, eng.progressEvery = f.eng.env, 0
	}
	return eng, err
}

// TASK-222: the VAD model of the speech component lies next to the program, outside the data root.
func TestTranscribeWithVAD(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	vad := filepath.Join(t.TempDir(), "speech component", "ggml-silero-v5.1.2.bin")
	writeTestFile(t, vad, "vad")

	var buf bytes.Buffer
	eng, err := newVADEngine(t, f, vad, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := outRef(t, "vad.json")
	if err := eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeEcho), out, shared.LoadMax, nil, nil); err != nil {
		t.Fatal(err)
	}
	segs := f.stored(t, out).Segments
	if len(segs) != 8 || segs[6].Text != "-oj -pp -ng --vad" || segs[7].Text != vad {
		t.Fatalf("fake saw %+v, want --vad and -vm with the model path", segs)
	}
	if buf.Len() != 0 {
		t.Fatalf("unexpected log %q", buf.String())
	}
}

func TestNewChecksTheVADModel(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "vad.bin")
	writeTestFile(t, file, "vad")

	tests := []struct {
		name    string
		vad     string
		wantErr error  // a configuration error
		want    string // the -vm path; "" without VAD
		warn    bool
	}{
		{name: "none", vad: "", want: ""},
		{name: "file", vad: file, want: file},
		{name: "relative", vad: "vad.bin", wantErr: ErrConfig},
		{name: "unclean", vad: dir + string(filepath.Separator) + "." + string(filepath.Separator) + "vad.bin", wantErr: ErrConfig},
		// The component verified the file; if it went away since, recognition still works, only slower.
		{name: "missing", vad: filepath.Join(dir, "gone.bin"), want: "", warn: true},
		{name: "directory", vad: dir, want: "", warn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, "")
			var buf bytes.Buffer
			eng, err := newVADEngine(t, f, tt.vad, &buf)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("New = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if eng.vad != tt.want {
				t.Errorf("vad = %q, want %q", eng.vad, tt.want)
			}
			if got := strings.Contains(buf.String(), "without voice activity detection"); got != tt.warn {
				t.Errorf("warned = %v, want %v; log %q", got, tt.warn, buf.String())
			}
			if strings.Contains(buf.String(), dir) {
				t.Errorf("the log names the path: %q", buf.String())
			}
		})
	}
}

// Speech of the synthetic lecture (testdata/vad: TTS phrases with long pauses, recognised by the pinned whisper-cli
// b5454 with base-q5_1 and --vad, 2026-10-27), in ms.
var vadSpeechStarts = []int64{2_000, 48_770, 131_920, 145_070, 168_980, 205_750}

// TASK-222: with VAD a segment before a pause ends where the next speech starts (up to 80 s later here), but the
// starts stay on the time line of the recording and in order. The live preview and the stored document take such
// segments as they are, and every phrase starts within a second of where it is spoken.
func TestVADFixtureKeepsTheTimeline(t *testing.T) {
	stdout, err := os.ReadFile(filepath.Join("testdata", "vad", "lecture.stdout"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "vad", "lecture.json"))
	if err != nil {
		t.Fatal(err)
	}

	doc, err := parseResult(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Segments) != 12 {
		t.Fatalf("%d segments in the result, want 12", len(doc.Segments))
	}

	// The CLI writes CRLF on Windows; the repository keeps LF.
	for _, input := range []string{string(stdout), strings.ReplaceAll(string(stdout), "\n", "\r\n")} {
		sink, live := collect()
		if _, err := sink.Write([]byte(input)); err != nil {
			t.Fatal(err)
		}
		if len(*live) != len(doc.Segments) {
			t.Fatalf("%d live segments, want %d", len(*live), len(doc.Segments))
		}

		batch := transcript.NewLiveBatch()
		now := time.Unix(0, 0)
		for i, s := range *live {
			if s.StartMs != doc.Segments[i].StartMs || s.EndMs != doc.Segments[i].EndMs {
				t.Errorf("live segment %d = %d..%d, result %d..%d", i, s.StartMs, s.EndMs, doc.Segments[i].StartMs, doc.Segments[i].EndMs)
			}
			if _, err := batch.Add(s, now); err != nil {
				t.Fatalf("live part %d refused: %v", i, err)
			}
		}
		if batch.Pending() != len(doc.Segments) {
			t.Fatalf("%d live parts pending, want %d", batch.Pending(), len(doc.Segments))
		}
	}

	segs := make([]transcript.Segment, len(doc.Segments))
	longest := int64(0)
	for i, s := range doc.Segments {
		segs[i] = transcript.Segment{StartMs: s.StartMs, EndMs: s.EndMs, Text: s.Text}
		longest = max(longest, s.EndMs-s.StartMs)
	}
	if _, err := transcript.NewDocument(transcript.Language(doc.Language), segs); err != nil {
		t.Fatalf("the document refuses the VAD segments: %v", err)
	}
	if longest < 60_000 {
		t.Fatalf("longest segment %d ms: the fixture should hold a segment stretched over a pause", longest)
	}

	// Phrases 0, 2, 4, 6, 8, 10 begin the spoken passages ("Good Morning!", "After the break", ...).
	for i, want := range vadSpeechStarts {
		got := doc.Segments[2*i].StartMs
		if d := got - want; d < -1000 || d > 1000 {
			t.Errorf("passage %d starts at %d ms, spoken at %d ms", i, got, want)
		}
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
