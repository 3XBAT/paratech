package values

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestNewSummaryRequest(t *testing.T) {
	prev, err := ParseHistorySummary(4, "сводка просьб 1–4")
	if err != nil {
		t.Fatal(err)
	}
	ok := []SummaryPair{{Student: "что задали", Reply: "reply: эссе"}}
	tests := []struct {
		name     string
		thread   string
		previous HistorySummary
		through  int
		pairs    []SummaryPair
		ok       bool
	}{
		{"without a summary", "th1", HistorySummary{}, 10, ok, true},
		{"after a summary", "th1", prev, 10, ok, true},
		{"empty reply", "th1", prev, 10, []SummaryPair{{Student: "текст"}}, true},
		{"limits exactly", "th1", prev, 10, []SummaryPair{{
			Student: strings.Repeat("я", MaxSummaryStudentBytes/2), Reply: strings.Repeat("ы", MaxSummaryReplyBytes/2),
		}}, true},
		{"ten pairs", "th1", prev, 14, make10(), true},
		{"bad thread", "a/b", prev, 10, ok, false},
		{"through not past the summary", "th1", prev, 4, ok, false},
		{"through zero", "th1", HistorySummary{}, 0, ok, false},
		{"no pairs", "th1", prev, 10, nil, false},
		{"eleven pairs", "th1", prev, 15, append(make10(), ok...), false},
		{"blank student", "th1", prev, 10, []SummaryPair{{Student: " \n ", Reply: "x"}}, false},
		{"student too long", "th1", prev, 10, []SummaryPair{{Student: strings.Repeat("a", MaxSummaryStudentBytes+1)}}, false},
		{"reply too long", "th1", prev, 10, []SummaryPair{{Student: "a", Reply: strings.Repeat("a", MaxSummaryReplyBytes+1)}}, false},
		{"NUL", "th1", prev, 10, []SummaryPair{{Student: "a\x00b"}}, false},
		{"invalid UTF-8", "th1", prev, 10, []SummaryPair{{Student: "a", Reply: "\xff"}}, false},
		// Ten pairs at their limits of quotes encode to twice their bytes: over 64 KiB.
		{"data too large", "th1", prev, 14, quoted(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewSummaryRequest(shared.ThreadID(tt.thread), tt.previous, tt.through, tt.pairs)
			if tt.ok != (err == nil) {
				t.Fatalf("NewSummaryRequest = %v, want ok %v", err, tt.ok)
			}
			if !tt.ok {
				if !errors.Is(err, ErrInvalidSummaryRequest) || !r.IsZero() {
					t.Fatalf("error %v, request %+v", err, r)
				}
				return
			}
			if r.IsZero() || r.Through() != tt.through || r.Thread() != shared.ThreadID(tt.thread) ||
				r.Previous() != tt.previous.Text() || len(r.Pairs()) != len(tt.pairs) {
				t.Fatalf("request = %+v", r)
			}
			if r.EncodedSize() != SummaryEncodedSize(tt.previous, tt.pairs) || r.EncodedSize() > MaxSummaryDataBytes {
				t.Fatalf("encoded size %d", r.EncodedSize())
			}
		})
	}
}

func make10() []SummaryPair {
	out := make([]SummaryPair, MaxSummaryPairs)
	for i := range out {
		out[i] = SummaryPair{Student: "просьба", Reply: "ответ"}
	}
	return out
}

func quoted() []SummaryPair {
	out := make([]SummaryPair, MaxSummaryPairs)
	for i := range out {
		out[i] = SummaryPair{Student: strings.Repeat(`"`, MaxSummaryStudentBytes), Reply: strings.Repeat(`\`, MaxSummaryReplyBytes)}
	}
	return out
}

func TestSummaryRequestKeyAndCopies(t *testing.T) {
	pairs := []SummaryPair{{Student: "a", Reply: "b"}}
	r, err := NewSummaryRequest("th1", HistorySummary{}, 12, pairs)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(`{"summary":"","pairs":[{"student":"a","reply":"b"}]}`))
	if want := "cs:th1:12:" + hex.EncodeToString(sum[:4]); r.Key() != want || r.Previous() != "" {
		t.Fatalf("key %q, want %q; previous %q", r.Key(), want, r.Previous())
	}
	pairs[0].Student = "changed"
	got := r.Pairs()
	got[0].Reply = "changed"
	if p := r.Pairs()[0]; p != (SummaryPair{Student: "a", Reply: "b"}) {
		t.Fatalf("the request shares its pairs: %+v", p)
	}
	if !(SummaryRequest{}).IsZero() {
		t.Fatal("the zero value is not zero")
	}
}

func TestSummaryRequestEqual(t *testing.T) {
	prev, err := ParseHistorySummary(4, "сводка")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ParseHistorySummary(4, "другая сводка")
	if err != nil {
		t.Fatal(err)
	}
	pairs := []SummaryPair{{Student: "a", Reply: "b"}, {Student: "c"}}
	build := func(thread shared.ThreadID, previous HistorySummary, through int, pairs []SummaryPair) SummaryRequest {
		t.Helper()
		r, err := NewSummaryRequest(thread, previous, through, pairs)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	base := build("th1", prev, 10, pairs)
	tests := []struct {
		name string
		r    SummaryRequest
		want bool
	}{
		{"same", build("th1", prev, 10, []SummaryPair{{Student: "a", Reply: "b"}, {Student: "c"}}), true},
		{"other thread", build("th2", prev, 10, pairs), false},
		{"other through", build("th1", prev, 11, pairs), false},
		{"other previous", build("th1", other, 10, pairs), false},
		{"no previous", build("th1", HistorySummary{}, 10, pairs), false},
		{"other reply", build("th1", prev, 10, []SummaryPair{{Student: "a", Reply: "x"}, {Student: "c"}}), false},
		{"fewer pairs", build("th1", prev, 10, pairs[:1]), false},
		{"zero", SummaryRequest{}, false},
	}
	for _, tt := range tests {
		if got := base.Equal(tt.r); got != tt.want || tt.r.Equal(base) != tt.want {
			t.Errorf("%s: Equal = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// The key names the chat, Through and the data: the same data is the same key, any other data another key. It stays
// within the Hub's key rules (1-128 printable ASCII without spaces).
func TestSummaryRequestKey(t *testing.T) {
	prev, err := ParseHistorySummary(4, "сводка")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ParseHistorySummary(4, "другая сводка")
	if err != nil {
		t.Fatal(err)
	}
	pairs := []SummaryPair{{Student: "a", Reply: "b"}, {Student: "c"}}
	build := func(thread shared.ThreadID, previous HistorySummary, through int, pairs []SummaryPair) SummaryRequest {
		t.Helper()
		r, err := NewSummaryRequest(thread, previous, through, pairs)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	base := build("th1", prev, 10, pairs)
	shape := regexp.MustCompile(`^cs:th1:10:[0-9a-f]{8}$`)
	if !shape.MatchString(base.Key()) {
		t.Fatalf("key %q", base.Key())
	}
	tests := []struct {
		name string
		r    SummaryRequest
		same bool
	}{
		{"same data", build("th1", prev, 10, []SummaryPair{{Student: "a", Reply: "b"}, {Student: "c"}}), true},
		{"other thread", build("th2", prev, 10, pairs), false},
		{"other through", build("th1", prev, 11, pairs), false},
		{"other previous", build("th1", other, 10, pairs), false},
		{"no previous", build("th1", HistorySummary{}, 10, pairs), false},
		{"other student", build("th1", prev, 10, []SummaryPair{{Student: "x", Reply: "b"}, {Student: "c"}}), false},
		{"other reply", build("th1", prev, 10, []SummaryPair{{Student: "a", Reply: "x"}, {Student: "c"}}), false},
		{"fewer pairs", build("th1", prev, 10, pairs[:1]), false},
	}
	for _, tt := range tests {
		if got := tt.r.Key() == base.Key(); got != tt.same {
			t.Errorf("%s: key %q vs %q, same = %v", tt.name, tt.r.Key(), base.Key(), got)
		}
	}

	// The longest thread id the desktop generates (26 characters) and a large Through stay far below 128.
	long := build(shared.ThreadID(strings.Repeat("z", 26)), HistorySummary{}, 1_000_000, pairs)
	if k := long.Key(); len(k) > 128 || strings.ContainsFunc(k, func(r rune) bool { return r <= ' ' || r > '~' }) {
		t.Fatalf("key %q breaks the Hub's key rules", k)
	}
}

// The size is that of the JSON as encoded without HTML escaping: escapes count, <>& do not grow.
func TestSummaryEncodedSize(t *testing.T) {
	prev, err := ParseHistorySummary(1, `a"b`)
	if err != nil {
		t.Fatal(err)
	}
	lineSeparator := string(rune(0x2028))
	tests := []struct {
		previous HistorySummary
		pairs    []SummaryPair
		want     string
	}{
		{HistorySummary{}, nil, `{"summary":"","pairs":[]}`},
		{prev, []SummaryPair{{Student: "<b>&\n", Reply: `\`}}, `{"summary":"a\"b","pairs":[{"student":"<b>&\n","reply":"\\"}]}`},
		// U+2028 is always escaped: six bytes on the wire for three of UTF-8.
		{HistorySummary{}, []SummaryPair{{Student: "я" + lineSeparator}}, `{"summary":"","pairs":[{"student":"я` + "\\" + `u2028","reply":""}]}`},
	}
	for _, tt := range tests {
		if got := SummaryEncodedSize(tt.previous, tt.pairs); got != len(tt.want) {
			t.Errorf("size of %+v = %d, want %d (%s)", tt.pairs, got, len(tt.want), tt.want)
		}
	}
}
