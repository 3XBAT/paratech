package lesson

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanText(t *testing.T) {
	tests := []struct {
		name, in string
		max      int
		want     string
	}{
		{"plain", "Иванов И. И.", 200, "Иванов И. И."},
		{"trimmed and collapsed", "  Иванов \t  И.  ", 200, "Иванов И."},
		{"line breaks become spaces", "Иванов\r\nИ.\u2028И.", 200, "Иванов И. И."},
		{"bidi controls dropped", "Iva\u202enov\u2066", 200, "Ivanov"},
		{"invalid utf-8 dropped", "Iva\xffnov", 200, "Ivanov"},
		{"cut to max runes", "абвгд", 3, "абв"},
		{"cut then trimmed", "аб вг", 3, "аб"},
		{"empty", " \n ", 200, ""},
	}
	for _, tt := range tests {
		if got := CleanText(tt.in, tt.max); got != tt.want {
			t.Errorf("%s: CleanText(%q, %d) = %q, want %q", tt.name, tt.in, tt.max, got, tt.want)
		}
	}
}

func TestValidateSubjectAndTeacher(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		valid bool
	}{
		{"empty", "", true},
		{"plain", "Петров П. П.", true},
		{"at limit", strings.Repeat("я", 200), true},
		{"too long", strings.Repeat("я", 201), false},
		{"tab", "Петров\tП.", false},
		{"double space", "Петров  П.", false},
		{"leading space", " Петров", false},
		{"bidi", "Петров\u200f", false},
	}
	for _, tt := range tests {
		for name, validate := range map[string]func(string) error{"subject": ValidateSubject, "teacher": ValidateTeacher} {
			err := validate(tt.in)
			if tt.valid && err != nil {
				t.Errorf("%s %s: %v", name, tt.name, err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidLesson) {
				t.Errorf("%s %s: err = %v, want ErrInvalidLesson", name, tt.name, err)
			}
		}
	}
}
