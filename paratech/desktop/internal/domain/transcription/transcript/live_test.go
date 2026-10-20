package transcript

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var liveT0 = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)

func TestNewPart(t *testing.T) {
	tests := []struct {
		name    string
		seq     int
		seg     Segment
		wantErr bool
	}{
		{name: "first", seq: 0, seg: Segment{0, 1000, "Привет"}},
		{name: "empty text", seq: 3, seg: Segment{5, 5, ""}},
		{name: "last number", seq: MaxSegments - 1, seg: Segment{0, 1, "x"}},
		{name: "negative number", seq: -1, seg: Segment{0, 1, "x"}, wantErr: true},
		{name: "number too big", seq: MaxSegments, seg: Segment{0, 1, "x"}, wantErr: true},
		{name: "end before start", seq: 0, seg: Segment{5, 1, "x"}, wantErr: true},
		{name: "out of range", seq: 0, seg: Segment{0, MaxOffsetMs + 1, "x"}, wantErr: true},
		{name: "control character", seq: 0, seg: Segment{0, 1, "a\x1bb"}, wantErr: true},
		{name: "invalid utf-8", seq: 0, seg: Segment{0, 1, "\xc3"}, wantErr: true},
		{name: "text too long", seq: 0, seg: Segment{0, 1, strings.Repeat("x", MaxSegmentTextBytes+1)}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPart(tc.seq, tc.seg)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDocument) {
					t.Fatalf("NewPart error = %v, want ErrInvalidDocument", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Seq() != tc.seq || p.Segment() != tc.seg {
				t.Fatalf("part = %d %+v", p.Seq(), p.Segment())
			}
		})
	}
}

func TestLiveBatchFlushesByCount(t *testing.T) {
	b := NewLiveBatch()
	for i := range LiveBatchParts {
		due, err := b.Add(Segment{int64(i), int64(i) + 1, "x"}, liveT0)
		if err != nil {
			t.Fatal(err)
		}
		if want := i == LiveBatchParts-1; due != want {
			t.Fatalf("part %d: due = %v, want %v", i, due, want)
		}
	}
	parts := b.Take()
	if len(parts) != LiveBatchParts || b.Pending() != 0 || b.Due(liveT0) {
		t.Fatalf("took %d, pending %d", len(parts), b.Pending())
	}
	for i, p := range parts {
		if p.Seq() != i {
			t.Fatalf("part %d has number %d", i, p.Seq())
		}
	}

	// Numbering goes on after a take.
	if _, err := b.Add(Segment{100, 101, "y"}, liveT0); err != nil {
		t.Fatal(err)
	}
	if got := b.Take(); len(got) != 1 || got[0].Seq() != LiveBatchParts {
		t.Fatalf("next part = %+v", got)
	}
}

func TestLiveBatchFlushesByTime(t *testing.T) {
	b := NewLiveBatch()
	if b.Due(liveT0) {
		t.Fatal("an empty batch is due")
	}
	if due, _ := b.Add(Segment{0, 1, "a"}, liveT0); due {
		t.Fatal("one fresh part is due")
	}
	if due, _ := b.Add(Segment{1, 2, "b"}, liveT0.Add(LiveFlushEvery-time.Millisecond)); due {
		t.Fatal("due before the interval of the oldest part")
	}
	if !b.Due(liveT0.Add(LiveFlushEvery)) {
		t.Fatal("not due after the interval of the oldest part")
	}
}

func TestLiveBatchRefusesBadSegments(t *testing.T) {
	b := NewLiveBatch()
	if _, err := b.Add(Segment{1000, 2000, "a"}, liveT0); err != nil {
		t.Fatal(err)
	}
	for _, s := range []Segment{{999, 2000, "earlier start"}, {1000, 900, "ends before start"}, {1000, 2000, "a\nb"}} {
		if _, err := b.Add(s, liveT0); !errors.Is(err, ErrInvalidDocument) {
			t.Fatalf("Add(%+v) error = %v, want ErrInvalidDocument", s, err)
		}
	}
	if _, err := b.Add(Segment{1000, 3000, "same start"}, liveT0); err != nil {
		t.Fatal(err)
	}
	parts := b.Take()
	if len(parts) != 2 || parts[1].Seq() != 1 {
		t.Fatalf("refused segments took numbers: %+v", parts)
	}
}

func TestLiveBatchRetain(t *testing.T) {
	b := NewLiveBatch()
	for i := range LiveBatchParts {
		_, _ = b.Add(Segment{int64(i), int64(i), "x"}, liveT0)
	}
	failed := b.Take()
	_, _ = b.Add(Segment{100, 100, "newer"}, liveT0)
	b.Retain(failed, liveT0)

	if b.Due(liveT0.Add(LiveFlushEvery - time.Millisecond)) {
		t.Fatal("retried before the interval")
	}
	if !b.Due(liveT0.Add(LiveFlushEvery)) {
		t.Fatal("not retried after the interval")
	}
	parts := b.Take()
	if len(parts) != LiveBatchParts+1 || parts[0].Seq() != 0 || parts[LiveBatchParts].Seq() != LiveBatchParts {
		t.Fatalf("retained order wrong: %d parts", len(parts))
	}
}

func TestLiveBatchRetainIsBounded(t *testing.T) {
	b := NewLiveBatch()
	var failed []Part
	for i := range maxLivePending + 10 {
		_, _ = b.Add(Segment{int64(i), int64(i), "x"}, liveT0)
		failed = append(failed, b.Take()...)
	}
	b.Retain(failed, liveT0)
	parts := b.Take()
	if len(parts) != maxLivePending || parts[0].Seq() != 10 {
		t.Fatalf("kept %d parts from %d, want the newest %d", len(parts), parts[0].Seq(), maxLivePending)
	}
}

// TASK-110: marks without speech take no number and are not stored; marks inside speech are removed.
func TestLiveBatchSkipsNonSpeech(t *testing.T) {
	b := NewLiveBatch()
	for _, s := range []Segment{{0, 1, "[BLANK_AUDIO]"}, {1, 2, " [MUSIC] Привет"}, {2, 3, "(музыка)"}, {3, 4, " всем"}} {
		if _, err := b.Add(s, liveT0); err != nil {
			t.Fatal(err)
		}
	}
	parts := b.Take()
	if len(parts) != 2 || parts[0].Seq() != 0 || parts[0].Segment() != (Segment{1, 2, "Привет"}) ||
		parts[1].Seq() != 1 || parts[1].Segment() != (Segment{3, 4, " всем"}) {
		t.Fatalf("parts = %+v", parts)
	}
}
