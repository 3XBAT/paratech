package transcribe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// segs returns n segments numbered in their text.
func segs(n int) []transcript.Segment {
	out := make([]transcript.Segment, n)
	for i := range out {
		out[i] = transcript.Segment{StartMs: int64(i) * 1000, EndMs: int64(i)*1000 + 900, Text: fmt.Sprintf("s%d", i)}
	}
	return out
}

func texts(ss []transcript.Segment) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Text
	}
	return out
}

// liveEngineFunc is an STTEngine that also reports live segments.
type liveEngineFunc func(ctx context.Context, out shared.ArtifactRef, progress func(int), segment func(transcript.Segment)) error

func (f liveEngineFunc) Transcribe(ctx context.Context, _, _, out shared.ArtifactRef, _ shared.RecognitionLoad,
	progress func(int), segment func(transcript.Segment)) error {
	return f(ctx, out, progress, segment)
}

func writeDoc(ctx context.Context, e *env, out shared.ArtifactRef) error {
	data, err := e.doc.Encode()
	if err != nil {
		return err
	}
	return e.artifacts.Write(ctx, out, bytes.NewReader(data))
}

// The parts are stored every LiveBatchParts segments and the rest at the end of the run; after the transcript is
// stored they are removed.
func TestLivePartsAreStoredInBatches(t *testing.T) {
	e := newEnv(t)
	all := segs(2*transcript.LiveBatchParts + 5)
	var during [][]string
	e.rebuild(t, liveEngineFunc(func(ctx context.Context, out shared.ArtifactRef, _ func(int), segment func(transcript.Segment)) error {
		for i, s := range all {
			segment(s)
			if i == len(all)-1 {
				got, _ := e.live.stored("rec-1")
				during = append(during, got)
			}
		}
		return writeDoc(ctx, e, out)
	}))
	var batchesAtSave []int
	e.transcripts.beforeSave = func(*fakeTranscripts) { _, batchesAtSave = e.live.stored("rec-1") }

	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if want := texts(all[:2*transcript.LiveBatchParts]); !reflect.DeepEqual(during[0], want) {
		t.Fatalf("stored during the run = %v, want the first two batches", during[0])
	}
	if want := []int{transcript.LiveBatchParts, transcript.LiveBatchParts, 5}; !reflect.DeepEqual(batchesAtSave, want) {
		t.Fatalf("batches = %v, want %v", batchesAtSave, want)
	}
	if got, _ := e.live.stored("rec-1"); len(got) != 0 {
		t.Fatalf("parts left after the transcript was stored: %v", got)
	}
	if got := e.live.deleted(); !reflect.DeepEqual(got, []shared.RecordingID{"rec-1"}) {
		t.Fatalf("deletes = %v", got)
	}
}

// A part waits at most LiveFlushEvery: the next segment or progress report after that stores it.
func TestLivePartsAreStoredAfterAnInterval(t *testing.T) {
	e := newEnv(t)
	var afterFirst, afterProgress, afterSecond int
	e.rebuild(t, liveEngineFunc(func(ctx context.Context, out shared.ArtifactRef, progress func(int), segment func(transcript.Segment)) error {
		ss := segs(3)
		segment(ss[0])
		_, b := e.live.stored("rec-1")
		afterFirst = len(b)

		e.clock.Advance(transcript.LiveFlushEvery)
		progress(10)
		_, b = e.live.stored("rec-1")
		afterProgress = len(b)

		segment(ss[1])
		e.clock.Advance(transcript.LiveFlushEvery)
		segment(ss[2])
		_, b = e.live.stored("rec-1")
		afterSecond = len(b)
		return writeDoc(ctx, e, out)
	}))

	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if afterFirst != 0 || afterProgress != 1 || afterSecond != 2 {
		t.Fatalf("stores after first %d, after progress %d, after the interval %d; want 0, 1, 2", afterFirst, afterProgress, afterSecond)
	}
}

// A failed store neither fails nor stops the transcription; the parts are retried and stored at the end.
func TestLiveStoreFailureIsNotFatal(t *testing.T) {
	e := newEnv(t)
	all := segs(transcript.LiveBatchParts + 3)
	e.live.failSaves(errors.New("database is locked"))
	e.rebuild(t, liveEngineFunc(func(ctx context.Context, out shared.ArtifactRef, _ func(int), segment func(transcript.Segment)) error {
		for _, s := range all {
			segment(s)
		}
		e.live.failSaves(nil)
		return writeDoc(ctx, e, out)
	}))
	e.live.deleteErr = errors.New("database is locked")

	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if e.transcripts.count() != 1 || len(e.events.list()) != 1 {
		t.Fatal("the transcription did not complete")
	}
	// The delete failed too, so the retried parts are still there.
	if got, _ := e.live.stored("rec-1"); !reflect.DeepEqual(got, texts(all)) {
		t.Fatalf("stored = %v, want every part after the retry", got)
	}
}

// A cancelled run keeps what it stored and stores what was pending; the next run numbers from 0 again and replaces
// the parts one by one, the rest stays until replaced.
func TestLivePartsSurviveCancellationAndRerun(t *testing.T) {
	e := newEnv(t)
	first := segs(transcript.LiveBatchParts + 2)
	started := make(chan struct{})
	e.rebuild(t, liveEngineFunc(func(ctx context.Context, _ shared.ArtifactRef, _ func(int), segment func(transcript.Segment)) error {
		for _, s := range first {
			segment(s)
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.svc.Handle(ctx, e.defaultJob(t), (&reporter{}).report) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Handle = %v, want a cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Handle did not return")
	}
	if got, _ := e.live.stored("rec-1"); !reflect.DeepEqual(got, texts(first)) {
		t.Fatalf("after the cancellation stored = %v", got)
	}

	// The rerun recognises differently and fails before the end.
	second := []transcript.Segment{{StartMs: 0, EndMs: 500, Text: "new0"}, {StartMs: 500, EndMs: 900, Text: "new1"}}
	e.rebuild(t, liveEngineFunc(func(_ context.Context, _ shared.ArtifactRef, _ func(int), segment func(transcript.Segment)) error {
		for _, s := range second {
			segment(s)
		}
		return errors.New("whisper-cli failed")
	}))
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err == nil {
		t.Fatal("Handle succeeded")
	}
	want := append([]string{"new0", "new1"}, texts(first[2:])...)
	if got, _ := e.live.stored("rec-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the rerun stored = %v, want %v", got, want)
	}
}

// Segments the domain refuses are left out and take no number.
func TestLiveRefusedSegmentsAreSkipped(t *testing.T) {
	e := newEnv(t)
	e.engine.SetSegments(
		transcript.Segment{StartMs: 1000, EndMs: 2000, Text: "a"},
		transcript.Segment{StartMs: 500, EndMs: 600, Text: "earlier"},
		transcript.Segment{StartMs: 2000, EndMs: 1000, Text: "backwards"},
		transcript.Segment{StartMs: 3000, EndMs: 4000, Text: "b"},
	)
	e.transcripts.beforeSave = func(*fakeTranscripts) {
		if got, _ := e.live.stored("rec-1"); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("stored = %v, want a and b", got)
		}
	}
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

// Without the LiveParts port the engine gets no segment callback.
func TestNoLivePartsPort(t *testing.T) {
	e := newEnv(t)
	var gotCallback bool
	svc, err := New(Deps{
		Engine: liveEngineFunc(func(ctx context.Context, out shared.ArtifactRef, _ func(int), segment func(transcript.Segment)) error {
			gotCallback = segment != nil
			return writeDoc(ctx, e, out)
		}),
		Transcripts: e.transcripts, Recordings: e.recordings, Models: e.models, Artifacts: e.artifacts,
		Events: e.events, Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if gotCallback {
		t.Fatal("the engine got a segment callback without a LiveParts port")
	}
}

// A transcript stored by an earlier attempt removes the parts again (idempotent).
func TestLivePartsRemovedWhenTranscriptExists(t *testing.T) {
	e := newEnv(t)
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatal(err)
	}
	if got := e.live.deleted(); len(got) != 2 {
		t.Fatalf("deletes = %v, want one per attempt", got)
	}
	if calls := e.engine.Calls(); len(calls) != 1 {
		t.Fatalf("engine calls = %d", len(calls))
	}
}
