package lesson

import (
	"errors"
	"testing"
)

func TestKindCodes(t *testing.T) {
	want := map[Kind]string{
		KindUnknown: "unknown", KindLecture: "lecture", KindSeminar: "seminar", KindPractice: "practice", KindLab: "lab",
	}
	if len(Kinds()) != len(want) {
		t.Fatalf("Kinds() = %v, want %d kinds", Kinds(), len(want))
	}
	for _, k := range Kinds() {
		if k.String() != want[k] {
			t.Errorf("%d.String() = %q, want %q", k, k.String(), want[k])
		}
		if err := k.Validate(); err != nil {
			t.Errorf("%v.Validate() = %v", k, err)
		}
		if got, err := KindOf(k.String()); err != nil || got != k {
			t.Errorf("KindOf(%q) = %v, %v; want %v", k.String(), got, err, k)
		}
	}
	if Kind(0) != KindUnknown {
		t.Error("the zero Kind is not KindUnknown")
	}
	if err := Kind(5).Validate(); !errors.Is(err, ErrInvalidLesson) {
		t.Errorf("Kind(5).Validate() = %v, want ErrInvalidLesson", err)
	}
	for _, code := range []string{"", "Lecture", "лекция", "lab "} {
		if _, err := KindOf(code); !errors.Is(err, ErrInvalidLesson) {
			t.Errorf("KindOf(%q) = %v, want ErrInvalidLesson", code, err)
		}
	}
}

func TestParseKind(t *testing.T) {
	tests := []struct {
		in   string
		want Kind
		ok   bool
	}{
		{"лекция", KindLecture, true},
		{"Лекция", KindLecture, true},
		{"ЛЕК.", KindLecture, true},
		{"лек", KindLecture, true},
		{" lecture ", KindLecture, true},
		{"Lec.", KindLecture, true},
		{"семинар", KindSeminar, true},
		{"Сем.", KindSeminar, true},
		{"Seminar", KindSeminar, true},
		{"sem", KindSeminar, true},
		{"практика", KindPractice, true},
		{"пр.", KindPractice, true},
		{"Практ.", KindPractice, true},
		{"practice", KindPractice, true},
		{"Practical", KindPractice, true},
		{"практическое занятие", KindPractice, true},
		{"лабораторная", KindLab, true},
		{"Лаб.", KindLab, true},
		{"лабораторная  работа", KindLab, true},
		{"лаб.раб.", KindLab, true},
		{"LAB", KindLab, true},
		{"laboratory", KindLab, true},
		{"другое", KindUnknown, true},
		{"unknown", KindUnknown, true},
		{"", KindUnknown, false},
		{"лекцияя", KindUnknown, false},
		{"лабиринт", KindUnknown, false},
		{"exam", KindUnknown, false},
		{"лек сем", KindUnknown, false},
	}
	for _, tt := range tests {
		got, ok := ParseKind(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseKind(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
