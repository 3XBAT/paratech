package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/transcription/transcript"
	tvalues "paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// TASK-110 on the real stack: whisper recognised only "[BLANK_AUDIO]" (the shape of the owner's 90-minute recording).
// The transcript is stored, the STT job fails at once with the no-speech error, no post-processing job is enqueued
// and the LLM is never called.
func TestNoSpeechTranscriptIsNotPostProcessed(t *testing.T) {
	segs := make([]transcript.Segment, 473)
	for i := range segs {
		segs[i] = transcript.Segment{StartMs: int64(i) * 10_000, EndMs: int64(i+1) * 10_000, Text: " [BLANK_AUDIO]"}
	}
	llm := testenv.NewFakeLLM()
	e := newPPEnvWith(t, llm, segs)
	e.start()

	var stt *job.Job
	testenv.Eventually(t, "stt job failed", func() bool {
		j, err := e.jobs.Get(context.Background(), "stt-1")
		stt = j
		return err == nil && j.Status() == job.StatusFailed
	}, func() { e.clock.Advance(time.Second) })
	if stt.Attempts() != 1 || !strings.HasSuffix(stt.LastError(), tvalues.NoSpeechMessage) {
		t.Fatalf("stt job: attempts %d, last error %q", stt.Attempts(), stt.LastError())
	}

	jobs, err := e.jobs.ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("%d jobs, want only the STT job", len(jobs))
	}
	if n := len(llm.Requests()); n != 0 {
		t.Fatalf("LLM requests = %d, want 0", n)
	}
	if _, err := e.summaries.Get(context.Background(), "stt-1"); err == nil {
		t.Fatal("a summary was stored")
	}
}
