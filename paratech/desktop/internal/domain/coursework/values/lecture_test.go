package values

import "testing"

func TestLessonKindValid(t *testing.T) {
	for _, k := range []LessonKind{LessonKindUnknown, LessonKindLecture, LessonKindSeminar, LessonKindPractice, LessonKindLab} {
		if !k.Valid() {
			t.Errorf("%q is not valid", k)
		}
	}
	for _, k := range []LessonKind{"exam", "Lecture", " lecture"} {
		if k.Valid() {
			t.Errorf("%q is valid", k)
		}
	}
}

func TestLessonKindOf(t *testing.T) {
	for code, want := range map[string]LessonKind{
		"lecture": LessonKindLecture, "seminar": LessonKindSeminar, "practice": LessonKindPractice, "lab": LessonKindLab,
		"unknown": LessonKindUnknown, "": LessonKindUnknown, "other": LessonKindUnknown, "Seminar": LessonKindUnknown,
		"exam": LessonKindUnknown,
	} {
		if got := LessonKindOf(code); got != want {
			t.Errorf("LessonKindOf(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestLectureTextCheckpoint(t *testing.T) {
	if got := LectureTextCheckpoint(3, 12); got != "3/12" {
		t.Fatalf("checkpoint = %q", got)
	}
	cases := []struct {
		in          string
		done, total int
		ok          bool
	}{
		{"3/12", 3, 12, true},
		{"0/1", 0, 1, true},
		{"12/12", 12, 12, true},
		{"", 0, 0, false},
		{"13/12", 0, 0, false},
		{"1/0", 0, 0, false},
		{"-1/3", 0, 0, false},
		{"01/3", 0, 0, false},
		{"+1/3", 0, 0, false},
		{"1/3/4", 0, 0, false},
		{"chunk 4", 0, 0, false},
	}
	for _, tc := range cases {
		done, total, ok := ParseLectureTextCheckpoint(tc.in)
		if done != tc.done || total != tc.total || ok != tc.ok {
			t.Errorf("Parse(%q) = %d, %d, %v; want %d, %d, %v", tc.in, done, total, ok, tc.done, tc.total, tc.ok)
		}
	}
}

func TestLectureTextStarted(t *testing.T) {
	for in, want := range map[string]bool{"3/12": true, "12/12": true, "1/1": true, "0/12": false, "": false, "x": false} {
		if got := LectureTextStarted(in); got != want {
			t.Errorf("LectureTextStarted(%q) = %v, want %v", in, got, want)
		}
	}
}
