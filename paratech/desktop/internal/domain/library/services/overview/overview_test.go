package overview

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

var msk = time.FixedZone("MSK", 3*60*60)

// now is Wednesday 21.10.2026, 12:00 in the student's zone.
var now = time.Date(2026, 10, 21, 12, 0, 0, 0, msk)

func TestNearest(t *testing.T) {
	at := func(days int, hour int) time.Time { return time.Date(2026, 10, 21+days, hour, 0, 0, 0, msk) }
	many := func(n int) []Deadline {
		var out []Deadline
		for i := range n {
			out = append(out, Deadline{Assignment: shared.AssignmentID(fmt.Sprintf("a%d", i)), Due: at(i+1, 10)})
		}
		return out
	}
	tests := []struct {
		name string
		in   []Deadline
		want []shared.AssignmentID
		days []int
	}{
		{name: "none", in: nil, want: []shared.AssignmentID{}, days: []int{}},
		{
			name: "passed and undated are left out",
			in: []Deadline{
				{Assignment: "passed", Due: at(0, 11)}, {Assignment: "undated"}, {Assignment: "later today", Due: at(0, 23)},
			},
			want: []shared.AssignmentID{"later today"}, days: []int{0},
		},
		{
			name: "soonest first, ties by id",
			in: []Deadline{
				{Assignment: "b", Due: at(6, 10)}, {Assignment: "c", Due: at(1, 9)}, {Assignment: "a", Due: at(6, 10)},
			},
			want: []shared.AssignmentID{"c", "a", "b"}, days: []int{1, 6, 6},
		},
		{name: "at most five", in: many(7), want: []shared.AssignmentID{"a0", "a1", "a2", "a3", "a4"}, days: []int{1, 2, 3, 4, 5}},
		{
			name: "days in the student's zone",
			// 22:30 UTC on the 21st is 01:30 on the 22nd in MSK: tomorrow.
			in:   []Deadline{{Assignment: "x", Due: time.Date(2026, 10, 21, 22, 30, 0, 0, time.UTC)}},
			want: []shared.AssignmentID{"x"}, days: []int{1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Nearest(tt.in, now)
			ids, days := []shared.AssignmentID{}, []int{}
			for _, d := range got {
				ids, days = append(ids, d.Assignment), append(days, d.DaysLeft)
			}
			if !slices.Equal(ids, tt.want) || !slices.Equal(days, tt.days) {
				t.Fatalf("Nearest = %v %v, want %v %v", ids, days, tt.want, tt.days)
			}
		})
	}
}

func TestSlots(t *testing.T) {
	lesson := func(kind string, day, hour, minute int) Scheduled {
		start := time.Date(2026, 10, day, hour, minute, 0, 0, msk)
		return Scheduled{Kind: kind, Start: start, End: start.Add(90 * time.Minute)}
	}
	tests := []struct {
		name string
		in   []Scheduled
		want []Slot
	}{
		{name: "none", in: nil, want: []Slot{}},
		{
			name: "weekly repeats collapse, Monday first",
			in: []Scheduled{
				lesson("lecture", 20, 10, 30), lesson("lecture", 27, 10, 30), // Tuesdays
				lesson("seminar", 19, 12, 10), lesson("seminar", 26, 12, 10), // Mondays
				lesson("lab", 25, 9, 0), // Sunday
			},
			want: []Slot{
				{Kind: "seminar", Weekday: time.Monday, StartMinute: 12*60 + 10, EndMinute: 13*60 + 40},
				{Kind: "lecture", Weekday: time.Tuesday, StartMinute: 10*60 + 30, EndMinute: 12 * 60},
				{Kind: "lab", Weekday: time.Sunday, StartMinute: 9 * 60, EndMinute: 10*60 + 30},
			},
		},
		{
			name: "same day: by start, then kind",
			in:   []Scheduled{lesson("seminar", 20, 14, 0), lesson("lecture", 20, 10, 30), lesson("lab", 20, 14, 0)},
			want: []Slot{
				{Kind: "lecture", Weekday: time.Tuesday, StartMinute: 10*60 + 30, EndMinute: 12 * 60},
				{Kind: "lab", Weekday: time.Tuesday, StartMinute: 14 * 60, EndMinute: 15*60 + 30},
				{Kind: "seminar", Weekday: time.Tuesday, StartMinute: 14 * 60, EndMinute: 15*60 + 30},
			},
		},
		{
			name: "read in the student's zone",
			// 22:00 UTC on Monday is 01:00 on Tuesday in MSK.
			in: []Scheduled{{Kind: "lecture", Start: time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC),
				End: time.Date(2026, 10, 19, 23, 30, 0, 0, time.UTC)}},
			want: []Slot{{Kind: "lecture", Weekday: time.Tuesday, StartMinute: 60, EndMinute: 150}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Slots(tt.in, msk); !slices.Equal(got, tt.want) {
				t.Fatalf("Slots = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSlotsAtMostMax(t *testing.T) {
	var in []Scheduled
	for i := range MaxSlots + 5 {
		start := time.Date(2026, 10, 19, 8, i, 0, 0, msk)
		in = append(in, Scheduled{Kind: "lecture", Start: start, End: start.Add(time.Hour)})
	}
	if got := Slots(in, msk); len(got) != MaxSlots {
		t.Fatalf("len(Slots) = %d, want %d", len(got), MaxSlots)
	}
}

func TestStatusOf(t *testing.T) {
	tests := []struct {
		in   Progress
		want Status
	}{
		{Progress{}, StatusRecorded},
		{Progress{Transcribed: true}, StatusTranscribed},
		{Progress{Transcribed: true, Summary: true}, StatusSummaryReady},
		{Progress{Failed: true}, StatusFailed},
		{Progress{Transcribed: true, Failed: true}, StatusFailed},
		{Progress{Transcribed: true, Summary: true, Failed: true}, StatusSummaryReady}, // a later step failed
		{Progress{Summary: true}, StatusRecorded},                                      // no transcript: the summary flag means nothing
	}
	for _, tt := range tests {
		if got := StatusOf(tt.in); got != tt.want {
			t.Errorf("StatusOf(%+v) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestFresh(t *testing.T) {
	opened := now.Add(-time.Hour)
	tests := []struct {
		name            string
		changed, opened time.Time
		want            bool
	}{
		{"never opened", now, time.Time{}, false},
		{"after the opening", now, opened, true},
		{"at the opening", opened, opened, false},
		{"before the opening", opened.Add(-time.Minute), opened, false},
	}
	for _, tt := range tests {
		if got := Fresh(tt.changed, tt.opened); got != tt.want {
			t.Errorf("%s: Fresh = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFreshMaterials(t *testing.T) {
	opened := now.Add(-time.Hour)
	mk := func(id string, status material.Status, updated time.Time) *material.Material {
		m, err := material.Restore(material.State{
			Version: 1, ID: shared.MaterialID(id), Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypeTXT,
			Name: id + ".txt", Subject: "s1", Folder: "f1", Status: status, Text: material.TextPending,
			CreatedAt: opened.Add(-24 * time.Hour), UpdatedAt: updated,
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	in := []*material.Material{
		mk("new", material.StatusActive, now), mk("old", material.StatusActive, opened.Add(-time.Minute)),
		mk("removed", material.StatusRemoved, now), mk("moved", material.StatusActive, opened.Add(time.Minute)),
	}
	var got []shared.MaterialID
	for _, m := range FreshMaterials(in, opened) {
		got = append(got, m.ID())
	}
	if want := []shared.MaterialID{"new", "moved"}; !slices.Equal(got, want) {
		t.Fatalf("FreshMaterials = %v, want %v", got, want)
	}
	if got := FreshMaterials(in, time.Time{}); len(got) != 0 {
		t.Fatalf("never opened: FreshMaterials = %d materials, want none", len(got))
	}
}
