package values

import (
	"errors"
	"testing"
	"time"
)

func TestKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Физика", "физика"},
		{"  ФИЗИКА  ", "физика"},
		{"Тёплые\tсети", "теплые сети"},
		{"ЁЛКИ", "елки"},
		{"Матем.\n анализ", "матем. анализ"},
		{"a​b", "ab"}, // a zero-width space is a format character
		{"\xff", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Key(tt.in); got != tt.want {
			t.Errorf("Key(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  Физика \r\n и  химия ", "Физика и химия"},
		{"a\x00b", "a b"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := CleanText(tt.in); got != tt.want {
			t.Errorf("CleanText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSemesterOf(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	tests := []struct {
		at    time.Time
		code  string
		label string
	}{
		{time.Date(2026, time.September, 1, 0, 0, 0, 0, msk), "2026-autumn", "Осень 2026"},
		{time.Date(2026, time.December, 31, 23, 0, 0, 0, msk), "2026-autumn", "Осень 2026"},
		{time.Date(2027, time.January, 20, 0, 0, 0, 0, msk), "2026-autumn", "Осень 2026"},
		{time.Date(2027, time.February, 1, 0, 0, 0, 0, msk), "2027-spring", "Весна 2027"},
		{time.Date(2027, time.August, 31, 23, 59, 0, 0, msk), "2027-spring", "Весна 2027"},
		// 31 August 22:00 UTC is already 1 September in Moscow: the date's own location decides.
		{time.Date(2026, time.August, 31, 22, 0, 0, 0, time.UTC).In(msk), "2026-autumn", "Осень 2026"},
	}
	for _, tt := range tests {
		s := SemesterOf(tt.at)
		if s.Code() != tt.code || s.Label() != tt.label {
			t.Errorf("SemesterOf(%v) = %q %q, want %q %q", tt.at, s.Code(), s.Label(), tt.code, tt.label)
		}
		back, err := ParseSemester(s.Code())
		if err != nil || back != s {
			t.Errorf("ParseSemester(%q) = %v, %v", s.Code(), back, err)
		}
	}
}

func TestParseSemesterRejects(t *testing.T) {
	for _, code := range []string{"", "2026", "2026-summer", "26-autumn", "1999-autumn", "2101-spring", "abcd-spring", "2026-Autumn", "+202-spring"} {
		if s, err := ParseSemester(code); !errors.Is(err, ErrInvalidSemester) {
			t.Errorf("ParseSemester(%q) = %v, %v; want ErrInvalidSemester", code, s, err)
		}
	}
	var zero Semester
	if !zero.IsZero() || zero.Code() != "" || zero.Label() != "" {
		t.Fatalf("zero semester: %q %q", zero.Code(), zero.Label())
	}
}
