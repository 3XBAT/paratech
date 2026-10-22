package post

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 21, 11, 52, 0, 0, time.UTC)

func TestNew(t *testing.T) {
	cases := []struct {
		name string
		rec  string
		e    Event
		tr   string
		now  time.Time
		ok   bool
	}{
		{"summary", "rec-1", EventSummary, "tr-1", t0, true},
		{"homework", "rec-1", EventHomework, "tr-1", t0, true},
		{"bad recording", "", EventSummary, "tr-1", t0, false},
		{"bad transcript", "rec-1", EventSummary, "", t0, false},
		{"unknown event", "rec-1", "lecture_text", "tr-1", t0, false},
		{"no time", "rec-1", EventSummary, "tr-1", time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(rec(tc.rec), tc.e, tr(tc.tr), tc.now)
			if tc.ok != (err == nil) {
				t.Fatalf("New: %v, want ok %v", err, tc.ok)
			}
			if !tc.ok {
				if !errors.Is(err, ErrInvalidPost) {
					t.Fatalf("New: %v, want ErrInvalidPost", err)
				}
				return
			}
			if p.SummaryReady() != (tc.e == EventSummary) || p.HomeworkChecked() != (tc.e == EventHomework) {
				t.Fatalf("flags summary %v homework %v after %s", p.SummaryReady(), p.HomeworkChecked(), tc.e)
			}
			if p.Version() != 0 || p.Recording() != rec(tc.rec) || p.Transcript() != tr(tc.tr) {
				t.Fatalf("post = %+v", p.State())
			}
		})
	}
}

func TestRecordIsIdempotent(t *testing.T) {
	p := mustNew(t, EventSummary, "tr-1")
	cases := []struct {
		name    string
		e       Event
		tr      string
		changed bool
		summary bool
		hw      bool
	}{
		{"same summary again", EventSummary, "tr-1", false, true, false},
		{"homework", EventHomework, "tr-1", true, true, true},
		{"homework again", EventHomework, "tr-1", false, true, true},
		{"summary again", EventSummary, "tr-1", false, true, true},
		{"new transcript drops the old results", EventHomework, "tr-2", true, false, true},
		{"new transcript summary", EventSummary, "tr-2", true, true, true},
	}
	now := t0
	for _, tc := range cases {
		now = now.Add(time.Minute)
		changed, err := p.Record(tc.e, tr(tc.tr), now)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if changed != tc.changed || p.SummaryReady() != tc.summary || p.HomeworkChecked() != tc.hw {
			t.Fatalf("%s: changed %v summary %v homework %v, want %v %v %v", tc.name, changed, p.SummaryReady(),
				p.HomeworkChecked(), tc.changed, tc.summary, tc.hw)
		}
	}
	if p.Transcript() != "tr-2" {
		t.Fatalf("transcript = %s, want tr-2", p.Transcript())
	}
}

func TestRecordRefusesBadInput(t *testing.T) {
	p := mustNew(t, EventSummary, "tr-1")
	for name, call := range map[string]func() (bool, error){
		"unknown event":  func() (bool, error) { return p.Record("draft", "tr-1", t0) },
		"bad transcript": func() (bool, error) { return p.Record(EventHomework, "a b", t0) },
		"no time":        func() (bool, error) { return p.Record(EventHomework, "tr-1", time.Time{}) },
	} {
		if _, err := call(); !errors.Is(err, ErrInvalidPost) {
			t.Errorf("%s: %v, want ErrInvalidPost", name, err)
		}
	}
	if p.HomeworkChecked() {
		t.Fatal("a refused event changed the post")
	}
}

func TestShownAt(t *testing.T) {
	later := t0.Add(time.Hour)
	cases := []struct {
		name        string
		first       Event
		second      Event // "" none
		assignments int
		want        time.Time
	}{
		{"summary only", EventSummary, "", 0, t0},
		{"homework found, no summary", EventHomework, "", 2, t0},
		{"no homework, no summary: hidden", EventHomework, "", 0, time.Time{}},
		{"homework first, summary later", EventHomework, EventSummary, 1, t0},
		{"summary first, homework later", EventSummary, EventHomework, 1, t0},
		{"homework first but none found", EventHomework, EventSummary, 0, later},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := mustNew(t, tc.first, "tr-1")
			if tc.second != "" {
				if _, err := p.Record(tc.second, "tr-1", later); err != nil {
					t.Fatal(err)
				}
			}
			if got := p.ShownAt(tc.assignments); !got.Equal(tc.want) {
				t.Fatalf("ShownAt = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRestore(t *testing.T) {
	good := State{Version: 2, Recording: "rec-1", Transcript: "tr-1", SummaryAt: t0, CreatedAt: t0, UpdatedAt: t0}
	if p, err := Restore(good); err != nil || p.State() != good {
		t.Fatalf("Restore(good) = %+v, %v", p, err)
	}
	cases := map[string]func(*State){
		"version 0":             func(s *State) { s.Version = 0 },
		"bad recording":         func(s *State) { s.Recording = "" },
		"bad transcript":        func(s *State) { s.Transcript = "" },
		"no creation time":      func(s *State) { s.CreatedAt = time.Time{} },
		"updated before create": func(s *State) { s.UpdatedAt = t0.Add(-time.Second) },
		"no event":              func(s *State) { s.SummaryAt = time.Time{} },
		"event after update":    func(s *State) { s.HomeworkAt = t0.Add(time.Second) },
	}
	for name, mutate := range cases {
		s := good
		mutate(&s)
		if _, err := Restore(s); !errors.Is(err, ErrInvalidPost) {
			t.Errorf("%s: %v, want ErrInvalidPost", name, err)
		}
	}
}

func rec(s string) shared.RecordingID { return shared.RecordingID(s) }

func tr(s string) shared.TranscriptID { return shared.TranscriptID(s) }

func mustNew(t *testing.T, e Event, transcript string) *Post {
	t.Helper()
	p, err := New("rec-1", e, tr(transcript), t0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
