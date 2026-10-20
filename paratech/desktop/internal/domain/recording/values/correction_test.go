package values

import (
	"errors"
	"strings"
	"testing"
)

func TestNewLessonCorrection(t *testing.T) {
	tests := []struct {
		name        string
		subject     string
		kind        string
		wantSubject string
		wantKind    string
		wantErr     bool
	}{
		{name: "empty corrects nothing"},
		{name: "subject and kind", subject: "Физика", kind: "lecture", wantSubject: "Физика", wantKind: "lecture"},
		{name: "kind in any case", kind: " Seminar ", wantKind: "seminar"},
		{name: "subject cleaned to one line", subject: " Мат\u202eанализ\n  2 ", wantSubject: "Матанализ 2"},
		{name: "longest subject", subject: strings.Repeat("ф", MaxLessonSubjectBytes/2), wantSubject: strings.Repeat("ф", MaxLessonSubjectBytes/2)},
		{name: "subject too long", subject: strings.Repeat("ф", MaxLessonSubjectBytes/2+1), wantErr: true},
		{name: "unknown kind", kind: "unknown", wantErr: true},
		{name: "kind not a code", kind: "лекция", wantErr: true},
		{name: "spaces only", subject: " \t ", kind: "  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewLessonCorrection(tc.subject, tc.kind)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidCorrection) {
					t.Fatalf("err = %v, want ErrInvalidCorrection", err)
				}
				return
			}
			if err != nil || c.Subject() != tc.wantSubject || c.Kind() != tc.wantKind {
				t.Fatalf("= %q, %q, %v; want %q, %q", c.Subject(), c.Kind(), err, tc.wantSubject, tc.wantKind)
			}
			if c.IsZero() != (tc.wantSubject == "" && tc.wantKind == "") {
				t.Errorf("IsZero = %v", c.IsZero())
			}
		})
	}
}

func TestLessonKindCodesAreACopy(t *testing.T) {
	codes := LessonKindCodes()
	codes[0] = "x"
	if LessonKindCodes()[0] != "lecture" {
		t.Fatal("LessonKindCodes shares its slice")
	}
}
