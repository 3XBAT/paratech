package lectureparts

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/lecturetext"
)

// speech returns n segments of text bytes each, one every stepMs.
func speech(n, bytes int, stepMs int64) []Segment {
	out := make([]Segment, n)
	for i := range out {
		out[i] = Segment{StartMs: int64(i) * stepMs, Text: fmt.Sprintf("%04d", i) + strings.Repeat("я", (bytes-4)/2)}
	}
	return out
}

func indexes(p Part) []int {
	var out []int
	for _, it := range p.Items() {
		out = append(out, it.Index)
	}
	return out
}

func TestPlan(t *testing.T) {
	cases := []struct {
		name     string
		segments []Segment
		parts    int
		check    func(t *testing.T, parts []Part)
	}{
		{
			name: "short transcript is one part", segments: speech(10, 100, 5000), parts: 1,
			check: func(t *testing.T, parts []Part) {
				if parts[0].Context() != "" || len(parts[0].Items()) != 10 {
					t.Fatalf("part = %d items, context %q", len(parts[0].Items()), parts[0].Context())
				}
			},
		},
		{
			// 101 bytes with the separator: 70 segments fit in 7 KB, and 70 s is far below the time limit.
			name: "cut by size", segments: speech(200, 100, 1000), parts: 3,
			check: func(t *testing.T, parts []Part) {
				for i, p := range parts {
					size := 0
					for _, it := range p.Items() {
						size += len(it.Text) + 1
					}
					if size > MaxPartBytes {
						t.Fatalf("part %d: %d bytes", i, size)
					}
				}
			},
		},
		{
			name: "cut by time", segments: speech(30, 20, 30_000), parts: 3, // 12 segments in 6 minutes
			check: func(t *testing.T, parts []Part) {
				if got := indexes(parts[1]); got[0] != 12 || len(got) != 12 {
					t.Fatalf("part 1 = %v", got)
				}
			},
		},
		{
			name: "boundaries keep every segment once and in order", segments: speech(500, 100, 3000), parts: 8,
			check: func(t *testing.T, parts []Part) {
				var all []int
				for _, p := range parts {
					all = append(all, indexes(p)...)
				}
				if len(all) != 500 || !slices.IsSorted(all) || all[0] != 0 || all[499] != 499 {
					t.Fatalf("segments across parts: %d, sorted %v", len(all), slices.IsSorted(all))
				}
			},
		},
		{
			name: "huge transcript grows the parts to the cap", segments: speech(9000, 100, 2000), parts: MaxParts,
		},
		{
			name:     "blank segments are skipped but keep their indexes",
			segments: []Segment{{0, " "}, {1000, "Первый."}, {2000, ""}, {3000, "Второй."}}, parts: 1,
			check: func(t *testing.T, parts []Part) {
				if got := indexes(parts[0]); !slices.Equal(got, []int{1, 3}) {
					t.Fatalf("indexes = %v", got)
				}
			},
		},
		{
			name:     "starts that go back are moved forward",
			segments: []Segment{{5000, "a"}, {3000, "b"}, {9000, "c"}}, parts: 1,
			check: func(t *testing.T, parts []Part) {
				items := parts[0].Items()
				if items[1].StartMs != 5000 || items[2].StartMs != 9000 {
					t.Fatalf("items = %+v", items)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := Plan(tc.segments)
			if err != nil {
				t.Fatal(err)
			}
			if tc.parts == MaxParts {
				if len(parts) > MaxParts || len(parts) < MaxParts/2 {
					t.Fatalf("parts = %d, want at most %d and not far below", len(parts), MaxParts)
				}
			} else if len(parts) != tc.parts {
				t.Fatalf("parts = %d, want %d", len(parts), tc.parts)
			}
			if tc.check != nil {
				tc.check(t, parts)
			}
		})
	}
}

func TestPlanThreeHourLecture(t *testing.T) {
	// About 300 KB of Cyrillic: a segment of 140 bytes every 5 s for 3 hours.
	parts, err := Plan(speech(3*3600/5, 140, 5000))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 30 || len(parts) > 50 {
		t.Fatalf("a 3-hour lecture gives %d parts, want about 40", len(parts))
	}
}

func TestPartContext(t *testing.T) {
	parts, err := Plan(speech(30, 20, 30_000)) // a cut every 12 segments (6 minutes)
	if err != nil {
		t.Fatal(err)
	}
	// The previous part ends with segment 11 at 330 s; segments from 305 s are context: only segment 11 (300 s is out).
	if got := parts[1].Context(); !strings.HasPrefix(got, "0011") || strings.Contains(got, "0010") {
		t.Fatalf("context = %q", got)
	}

	dense, err := Plan(speech(200, 100, 1000))
	if err != nil {
		t.Fatal(err)
	}
	ctx := dense[1].Context()
	last := dense[0].Items()[len(dense[0].Items())-1].Text
	if !strings.HasSuffix(ctx, last) || len(ctx) > MaxContextBytes {
		t.Fatalf("context of %d bytes does not end with the previous part", len(ctx))
	}
	for _, it := range dense[1].Items() {
		if strings.Contains(ctx, it.Text) {
			t.Fatal("the context repeats a segment of the part")
		}
	}
}

func TestPlanRejects(t *testing.T) {
	cases := []struct {
		name     string
		segments []Segment
		want     error
	}{
		{"empty", nil, ErrEmpty},
		{"blank", []Segment{{0, " "}, {1, "\t"}}, ErrEmpty},
		{"newline", []Segment{{0, "a\nb"}}, ErrInvalidSegment},
		{"invalid utf8", []Segment{{0, "\xff"}}, ErrInvalidSegment},
	}
	for _, tc := range cases {
		if _, err := Plan(tc.segments); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestStarts(t *testing.T) {
	p := Part{items: []Item{{Index: 10, StartMs: 60_000}, {Index: 11, StartMs: 65_000}, {Index: 13, StartMs: 70_000}}}
	cases := []struct {
		name  string
		froms []int
		want  []int64
	}{
		{"inside", []int{10, 11, 13}, []int64{60_000, 65_000, 70_000}},
		{"blank segment inside the part", []int{12}, []int64{65_000}},
		{"before the part", []int{3}, []int64{60_000}},
		{"after the part", []int{99}, []int64{60_000}},
		{"going back", []int{13, 11}, []int64{70_000, 70_000}},
		{"out of range after a valid one", []int{11, 99}, []int64{65_000, 65_000}},
		{"none", nil, []int64{}},
	}
	for _, tc := range cases {
		if got := p.Starts(tc.froms); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Starts(%v) = %v, want %v", tc.name, tc.froms, got, tc.want)
		}
	}
}

func blk(t *testing.T, heading string, start int64, paragraphs ...string) lecturetext.Block {
	t.Helper()
	b, err := lecturetext.NewBlock(heading, start, paragraphs)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMerge(t *testing.T) {
	type want struct {
		heading    string
		start      int64
		paragraphs []string
	}
	cases := []struct {
		name  string
		parts [][]lecturetext.Block
		want  []want
	}{
		{"empty", nil, nil},
		{
			"concatenates in order",
			[][]lecturetext.Block{{blk(t, "A", 0, "a1")}, {blk(t, "B", 10, "b1"), blk(t, "C", 20, "c1")}},
			[]want{{"A", 0, []string{"a1"}}, {"B", 10, []string{"b1"}}, {"C", 20, []string{"c1"}}},
		},
		{
			"drops the overlap duplicate",
			[][]lecturetext.Block{{blk(t, "A", 0, "a1", "Итак, дельты.")}, {blk(t, "B", 10, "  итак,   ДЕЛЬТЫ. ", "b2")}},
			[]want{{"A", 0, []string{"a1", "Итак, дельты."}}, {"B", 10, []string{"b2"}}},
		},
		{
			"a first block left empty is dropped",
			[][]lecturetext.Block{{blk(t, "A", 0, "same")}, {blk(t, "B", 10, "same"), blk(t, "C", 20, "c1")}},
			[]want{{"A", 0, []string{"same"}}, {"C", 20, []string{"c1"}}},
		},
		{
			"joins a block that continues the heading",
			[][]lecturetext.Block{{blk(t, "Дельты", 0, "a1")}, {blk(t, " дельты ", 10, "a2"), blk(t, "B", 20, "b1")}},
			[]want{{"Дельты", 0, []string{"a1", "a2"}}, {"B", 20, []string{"b1"}}},
		},
		{
			"only the first block of a part is joined",
			[][]lecturetext.Block{{blk(t, "A", 0, "a1"), blk(t, "A", 5, "a2")}},
			[]want{{"A", 0, []string{"a1"}}, {"A", 5, []string{"a2"}}},
		},
		{
			"an empty part changes nothing",
			[][]lecturetext.Block{{blk(t, "A", 0, "a1")}, nil, {blk(t, "A", 20, "a1", "a2")}},
			[]want{{"A", 0, []string{"a1", "a2"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Merge(tc.parts)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("blocks = %d, want %d", len(got), len(tc.want))
			}
			for i, w := range tc.want {
				g := got[i]
				if g.Heading() != w.heading || g.StartMs() != w.start || !slices.Equal(g.Paragraphs(), w.paragraphs) {
					t.Fatalf("block %d = %+v, want %+v", i, g.State(), w)
				}
			}
		})
	}
}

// A join never makes a block the aggregate refuses.
func TestMergeKeepsTheParagraphLimit(t *testing.T) {
	full := make([]string, lecturetext.MaxParagraphs)
	for i := range full {
		full[i] = fmt.Sprint("p", i)
	}
	got, err := Merge([][]lecturetext.Block{{blk(t, "A", 0, full...)}, {blk(t, "A", 10, "more")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Paragraphs()) != lecturetext.MaxParagraphs {
		t.Fatalf("blocks = %d", len(got))
	}
}

func TestCondensed(t *testing.T) {
	// One part of 10 segments of 100 runes (Cyrillic: 2 bytes each) = 1000 runes.
	seg := strings.Repeat("я", 100)
	big := make([]Segment, 10)
	for i := range big {
		big[i] = Segment{StartMs: int64(i) * 1000, Text: seg}
	}
	tiny := []Segment{{Text: strings.Repeat("я", MinCheckedRunes-1)}}
	tests := []struct {
		name       string
		segments   []Segment
		paragraphs []string
		want       bool
	}{
		{"full", big, []string{strings.Repeat("я", 600), strings.Repeat("я", 400)}, false},
		{"at the threshold", big, []string{strings.Repeat("я", 450)}, false},
		{"below the threshold", big, []string{strings.Repeat("я", 449)}, true},
		{"a summary", big, []string{"Коротко о реках."}, true},
		{"no paragraphs", big, nil, true},
		{"a tiny part is not checked", tiny, []string{"Да."}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := Plan(tt.segments)
			if err != nil || len(plan) != 1 {
				t.Fatalf("Plan = %d parts, %v", len(plan), err)
			}
			if got := plan[0].Condensed(tt.paragraphs); got != tt.want {
				t.Fatalf("Condensed = %v, want %v", got, tt.want)
			}
		})
	}
}
