package transcribe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// blankDocument is the shape of the owner's case (TASK-110): every segment is whisper's "[BLANK_AUDIO]".
func blankDocument(t *testing.T, n int) transcript.Document {
	t.Helper()
	segs := make([]transcript.Segment, n)
	for i := range segs {
		segs[i] = transcript.Segment{StartMs: int64(i) * 10_000, EndMs: int64(i+1) * 10_000, Text: " [BLANK_AUDIO]"}
	}
	doc, err := transcript.NewDocument("en", segs)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// TASK-110: a transcript without speech is stored, but not published, so nothing is post-processed and the Hub is
// not called; the job fails at once with the fixed no-speech error. A rerun of the job does not publish either.
func TestNoSpeechIsNotPublished(t *testing.T) {
	tests := []struct {
		name string
		segs []transcript.Segment
	}{
		{name: "only blank audio", segs: blankDocument(t, 473).Segments()},
		{name: "only marks", segs: []transcript.Segment{
			{StartMs: 0, EndMs: 1, Text: "[ Silence ]"}, {StartMs: 1, EndMs: 2, Text: "(музыка)"}, {StartMs: 2, EndMs: 3, Text: "♪♪"},
		}},
		{name: "no segments", segs: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			doc, err := transcript.NewDocument("en", tc.segs)
			if err != nil {
				t.Fatal(err)
			}
			e.engine = testenv.NewFakeSTT(e.artifacts, doc)
			e.rebuild(t, e.engine)

			snap := e.defaultJob(t)
			for attempt := range 2 {
				err := e.svc.Handle(context.Background(), snap, (&reporter{}).report)
				if !errors.Is(err, ErrNoSpeech) || backoff.Classify(err) != backoff.ClassPermanent {
					t.Fatalf("attempt %d: Handle = %v, want permanent ErrNoSpeech", attempt, err)
				}
				// The recordings screen recognises the job's last error by this suffix.
				if !strings.HasSuffix(err.Error(), values.NoSpeechMessage) {
					t.Fatalf("attempt %d: message %q", attempt, err.Error())
				}
			}
			if got := e.events.list(); len(got) != 0 {
				t.Fatalf("events = %+v, want none", got)
			}
			if e.transcripts.count() != 1 || len(e.engine.Calls()) != 1 {
				t.Fatalf("transcripts %d, engine calls %d; want 1 and 1", e.transcripts.count(), len(e.engine.Calls()))
			}
			if got := e.live.deleted(); len(got) == 0 {
				t.Fatal("live parts not removed")
			}
		})
	}
}

// Speech among the marks is published as before.
func TestSpeechAmongMarksIsPublished(t *testing.T) {
	e := newEnv(t)
	segs := append(blankDocument(t, 10).Segments(), transcript.Segment{StartMs: 100_000, EndMs: 101_000, Text: " Начнём."})
	doc, err := transcript.NewDocument("ru", segs)
	if err != nil {
		t.Fatal(err)
	}
	e.engine = testenv.NewFakeSTT(e.artifacts, doc)
	e.rebuild(t, e.engine)
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := e.events.list(); len(got) != 1 {
		t.Fatalf("events = %+v, want one", got)
	}
}
