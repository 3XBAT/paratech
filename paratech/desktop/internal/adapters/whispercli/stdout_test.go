package whispercli

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

// collect returns a sink that records the segments it parses.
func collect() (*stdoutSink, *[]transcript.Segment) {
	var got []transcript.Segment
	return newStdoutSink(func(s transcript.Segment) { got = append(got, s) }), &got
}

func TestStdoutSinkParsesSegmentLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []transcript.Segment
	}{
		{
			name:  "cli line",
			input: "\n[00:00:00.000 --> 00:00:05.000]   Hello, world.\n",
			want:  []transcript.Segment{{StartMs: 0, EndMs: 5000, Text: "Hello, world."}},
		},
		{
			name:  "cyrillic and crlf",
			input: "[00:01:02.340 --> 00:01:04.000]  Привет, мир.\r\n[00:01:04.000 --> 00:01:06.500]  Пока\r\n",
			want: []transcript.Segment{
				{StartMs: 62_340, EndMs: 64_000, Text: "Привет, мир."},
				{StartMs: 64_000, EndMs: 66_500, Text: "Пока"},
			},
		},
		{
			name:  "hours",
			input: "[02:59:59.999 --> 03:00:01.000]  late\n",
			want:  []transcript.Segment{{StartMs: 10_799_999, EndMs: 10_801_000, Text: "late"}},
		},
		{
			name: "garbage dropped",
			input: "whisper_print_progress_callback: progress =  5%\n" +
				"[00:00:00.000 -> 00:00:05.000]  arrow\n" +
				"[0:00:00.000 --> 00:00:05.000]  short hours\n" +
				"[00:61:00.000 --> 00:00:05.000]  minutes\n" +
				"[00:00:00.00 --> 00:00:05.000]  milliseconds\n" +
				" [00:00:00.000 --> 00:00:05.000]  leading space\n" +
				"[0000:00:00.000 --> 00:00:05.000]  four digit hours\n",
			want: nil,
		},
		{
			name:  "control characters and invalid utf-8",
			input: "[00:00:00.000 --> 00:00:01.000]  a\tb\x1bc \xff end\n",
			want:  []transcript.Segment{{StartMs: 0, EndMs: 1000, Text: "a b c � end"}},
		},
		{
			name:  "empty text skipped",
			input: "[00:00:00.000 --> 00:00:01.000]   \n[00:00:01.000 --> 00:00:02.000]\n",
			want:  nil,
		},
		{
			name: "order clamped like the result",
			input: "[00:00:10.000 --> 00:00:12.000]  first\n" +
				"[00:00:05.000 --> 00:00:06.000]  earlier\n" +
				"[00:00:20.000 --> 00:00:15.000]  ends before start\n",
			want: []transcript.Segment{
				{StartMs: 10_000, EndMs: 12_000, Text: "first"},
				{StartMs: 10_000, EndMs: 10_000, Text: "earlier"},
				{StartMs: 20_000, EndMs: 20_000, Text: "ends before start"},
			},
		},
		{
			name:  "beyond a day",
			input: "[24:00:00.001 --> 24:00:01.000]  too late\n",
			want:  nil,
		},
		{
			name:  "unterminated last line waits",
			input: "[00:00:00.000 --> 00:00:01.000]  pending",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, got := collect()
			if _, err := s.Write([]byte(tc.input)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Fatalf("segments = %#v, want %#v", *got, tc.want)
			}
		})
	}
}

// Writes of any size give the same segments: lines and UTF-8 characters may be split across writes.
func TestStdoutSinkSplitWrites(t *testing.T) {
	input := "[00:00:00.000 --> 00:00:02.000]  Съешь же ещё этих мягких булок\n[00:00:02.000 --> 00:00:03.000]  да выпей чаю\r\n"
	want := []transcript.Segment{
		{StartMs: 0, EndMs: 2000, Text: "Съешь же ещё этих мягких булок"},
		{StartMs: 2000, EndMs: 3000, Text: "да выпей чаю"},
	}
	for _, size := range []int{1, 2, 3, 7, 64} {
		s, got := collect()
		for i := 0; i < len(input); i += size {
			_, _ = s.Write([]byte(input[i:min(i+size, len(input))]))
		}
		if !reflect.DeepEqual(*got, want) {
			t.Fatalf("chunks of %d: segments = %#v", size, *got)
		}
	}
}

func TestStdoutSinkDropsOverlongLines(t *testing.T) {
	s, got := collect()
	long := "[00:00:00.000 --> 00:00:01.000]  " + strings.Repeat("x", maxStdoutLine) + "\n"
	ok := "[00:00:01.000 --> 00:00:02.000]  " + strings.Repeat("y", maxSegmentText) + "\n"
	for _, line := range []string{long, ok} {
		for i := 0; i < len(line); i += 4096 {
			_, _ = s.Write([]byte(line[i:min(i+4096, len(line))]))
		}
	}
	if len(*got) != 1 || (*got)[0].StartMs != 1000 || len((*got)[0].Text) != maxSegmentText {
		t.Fatalf("got %d segments, want only the line within the limit", len(*got))
	}
	if cap(s.line) > maxStdoutLine {
		t.Fatalf("line buffer grew to %d bytes", cap(s.line))
	}
}

func TestStdoutSinkStop(t *testing.T) {
	s, got := collect()
	_, _ = s.Write([]byte("[00:00:00.000 --> 00:00:01.000]  half"))
	s.stop()
	n, err := s.Write([]byte(" line\n[00:00:01.000 --> 00:00:02.000]  after\n"))
	if err != nil || n == 0 {
		t.Fatalf("write after stop = %d, %v", n, err)
	}
	if len(*got) != 0 {
		t.Fatalf("segments after stop: %#v", *got)
	}
}

func TestSegmentPumpOrderAndStop(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	var mu sync.Mutex
	var got []int64
	release := make(chan struct{})
	p := startSegmentPump(func(s transcript.Segment) {
		<-release
		mu.Lock()
		got = append(got, s.StartMs)
		mu.Unlock()
	})

	// A blocked callback does not block send.
	done := make(chan struct{})
	go func() {
		for i := range 100 {
			p.send(transcript.Segment{StartMs: int64(i), Text: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("send blocked on a slow callback")
	}
	close(release)
	p.stop()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 100 {
		t.Fatalf("delivered %d segments, want 100", len(got))
	}
	for i, v := range got {
		if v != int64(i) {
			t.Fatalf("segment %d delivered as %d: order lost", i, v)
		}
	}
}

func TestSegmentPumpIsBounded(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	delivered := 0
	p := startSegmentPump(func(transcript.Segment) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		delivered++
		mu.Unlock()
	})

	// The first segment holds the callback; then the queue takes maxQueuedSegments and drops the rest.
	p.send(transcript.Segment{Text: "first"})
	<-entered
	for i := range maxQueuedSegments + 50 {
		p.send(transcript.Segment{StartMs: int64(i), Text: "x"})
	}
	close(release)
	p.stop()

	mu.Lock()
	defer mu.Unlock()
	if delivered != maxQueuedSegments+1 || p.droppedCount() != 50 {
		t.Fatalf("delivered %d, dropped %d: want %d and 50", delivered, p.droppedCount(), maxQueuedSegments+1)
	}
}

func TestSegmentPumpBoundsBytes(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var got []int
	p := startSegmentPump(func(s transcript.Segment) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		got = append(got, len(s.Text))
	})

	p.send(transcript.Segment{Text: "first"})
	<-entered
	half := strings.Repeat("y", maxQueuedBytes/2)
	p.send(transcript.Segment{Text: half})
	p.send(transcript.Segment{Text: half})
	p.send(transcript.Segment{Text: "z"}) // over the byte bound
	close(release)
	p.stop()

	if !reflect.DeepEqual(got, []int{5, len(half), len(half)}) || p.droppedCount() != 1 {
		t.Fatalf("delivered lengths %v, dropped %d", got, p.droppedCount())
	}
}

func TestTranscribeDeliversLiveSegments(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	out := outRef(t, "rec-1/live.json")

	var got []transcript.Segment
	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeLive), out, nil,
		func(s transcript.Segment) { got = append(got, s) })
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	want := []transcript.Segment{
		{StartMs: 0, EndMs: 2500, Text: "Привет, мир."},
		{StartMs: 2500, EndMs: 4000, Text: "Пока"},
		{StartMs: 2500, EndMs: 2500, Text: "earlier start"},
		{StartMs: 6000, EndMs: 7000, Text: "bad � byte"},
		{StartMs: 3_723_004, EndMs: 3_725_000, Text: "last"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments = %#v\nwant %#v", got, want)
	}

	// The stored document is still the result file, not the live lines.
	doc := f.stored(t, out)
	if len(doc.Segments) != 2 || doc.Segments[1].Text != "Пока" {
		t.Fatalf("document = %+v", doc)
	}
}

// Without a segment callback stdout goes to the null device, as before.
func TestTranscribeWithoutSegmentCallback(t *testing.T) {
	f := newFixture(t, "")
	out := outRef(t, "rec-1/quiet.json")
	if err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeLive), out, nil, nil); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
}
