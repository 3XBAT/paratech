package transcriptview

import (
	"context"
	"testing"

	"paratech/desktop/internal/domain/transcription/transcript"
)

// TASK-110: whisper's marks are neither shown nor exported, also from a transcript stored before the rule; a
// transcript of marks only shows no segments.
func TestMarksAreNotShown(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "Физика")
	e.addRecording(t, "r2", "")
	e.addTranscript(t, "r1", []transcript.Segment{
		{StartMs: 0, EndMs: 1000, Text: " [BLANK_AUDIO]"}, {StartMs: 1000, EndMs: 2000, Text: " [MUSIC] Начнём."},
		{StartMs: 2000, EndMs: 3000, Text: "(музыка)"}, {StartMs: 3000, EndMs: 4000, Text: " (x + 1)"},
	})
	blank := make([]transcript.Segment, 473)
	for i := range blank {
		blank[i] = transcript.Segment{StartMs: int64(i) * 1000, EndMs: int64(i)*1000 + 900, Text: " [BLANK_AUDIO]"}
	}
	e.addTranscript(t, "r2", blank)
	ctx := context.Background()

	page, err := e.svc.Segments(ctx, "r1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []transcript.Segment{{StartMs: 1000, EndMs: 2000, Text: "Начнём."}, {StartMs: 3000, EndMs: 4000, Text: " (x + 1)"}}
	if page.Total != 2 || len(page.Segments) != 2 || page.Segments[0] != want[0] || page.Segments[1] != want[1] {
		t.Fatalf("page = %+v", page)
	}
	if got, err := e.svc.Text(ctx, "r1", false); err != nil || got != "Начнём.\n(x + 1)\n" {
		t.Fatalf("text = %q, %v", got, err)
	}
	if page, err := e.svc.Segments(ctx, "r2", 0, 10); err != nil || page.Total != 0 || len(page.Segments) != 0 || !page.Final {
		t.Fatalf("marks only: page = %+v, %v", page, err)
	}
}

// Live parts stored before the rule hold marks: the text leaves them out.
func TestMarksInLivePartsAreNotCopied(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	e.addParts(t, "r1", []transcript.Segment{{StartMs: 0, EndMs: 1, Text: "[BLANK_AUDIO]"}, {StartMs: 1, EndMs: 2, Text: " итог"}})
	if got, err := e.svc.Text(context.Background(), "r1", false); err != nil || got != "итог\n" {
		t.Fatalf("text = %q, %v", got, err)
	}
}
