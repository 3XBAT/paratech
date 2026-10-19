package values

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCleanName(t *testing.T) {
	long := strings.Repeat("я", MaxNameRunes+10)
	cases := []struct {
		name, in, want string
	}{
		{"plain", "Матанализ", "Матанализ"},
		{"spaces collapsed", "  Иванов \t И.\u00a0И.  ", "Иванов И. И."},
		{"line breaks", "Физика\r\nSubject: x", "Физика Subject: x"},
		{"line separator", "a\u2028b\u2029c", "a b c"},
		{"bidi and zero width", "ab\u202ecd\u200b\u2066e", "abcde"},
		{"control", "a\x00b\x1bc", "a b c"},
		{"invalid utf-8", "a\xffb", "ab"},
		{"only junk", "\u202e\r\n", ""},
		{"cut", long, strings.Repeat("я", MaxNameRunes)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanName(tc.in)
			if got != tc.want {
				t.Fatalf("CleanName(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if again := CleanName(got); again != got {
				t.Fatalf("cleaning again changed %q to %q", got, again)
			}
		})
	}
}

func TestNewLessonLabelPrecedence(t *testing.T) {
	start := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	sched := ScheduledLesson{Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов", Start: start}
	hints := NewLessonNames("Алгебра", "Петров")
	cases := []struct {
		name       string
		sched      ScheduledLesson
		hints      LessonNames
		correction LessonNames
		subject    string
		subjectSrc NameSource
		teacher    string
		teacherSrc NameSource
	}{
		{"schedule wins over hints", sched, hints, LessonNames{}, "Матанализ", SourceSchedule, "Иванов", SourceSchedule},
		{"hints fill the gaps", ScheduledLesson{Kind: "seminar"}, hints, LessonNames{}, "Алгебра", SourceTranscript, "Петров", SourceTranscript},
		{"hint only for the missing field", ScheduledLesson{Subject: "Матанализ"}, hints, LessonNames{}, "Матанализ", SourceSchedule, "Петров", SourceTranscript},
		{"correction wins", sched, hints, NewLessonNames("Геометрия", "Сидоров"), "Геометрия", SourceUser, "Сидоров", SourceUser},
		{"partial correction", sched, hints, NewLessonNames("", "Сидоров"), "Матанализ", SourceSchedule, "Сидоров", SourceUser},
		{"nothing", ScheduledLesson{}, LessonNames{}, LessonNames{}, "", SourceNone, "", SourceNone},
		{"blank schedule text is no schedule", ScheduledLesson{Subject: " \u202e "}, hints, LessonNames{}, "Алгебра", SourceTranscript, "Петров", SourceTranscript},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLessonLabel(tc.sched, tc.hints, tc.correction)
			if l.Subject() != tc.subject || l.SubjectSource() != tc.subjectSrc || l.Teacher() != tc.teacher || l.TeacherSource() != tc.teacherSrc {
				t.Fatalf("label = %q (%s), %q (%s)", l.Subject(), l.SubjectSource(), l.Teacher(), l.TeacherSource())
			}
			if l.Kind() != tc.sched.Kind || !l.Date().Equal(tc.sched.Start) {
				t.Fatalf("kind %q, date %v", l.Kind(), l.Date())
			}
		})
	}
	if !NewLessonLabel(ScheduledLesson{}, LessonNames{}, LessonNames{}).IsZero() {
		t.Fatal("an empty label is not zero")
	}
}

func TestNewLessonLabelKind(t *testing.T) {
	for in, want := range map[string]string{
		"lecture": "lecture", "lab": "lab", "": "", "Lecture": "", "лекция": "", "a b": "", strings.Repeat("a", 33): "",
	} {
		if got := NewLessonLabel(ScheduledLesson{Kind: in}, LessonNames{}, LessonNames{}).Kind(); got != want {
			t.Errorf("kind %q = %q, want %q", in, got, want)
		}
	}
}

func TestHomeworkEmailSubject(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	due := time.Date(2026, 10, 26, 22, 30, 0, 0, time.UTC).In(msk) // 27.10 in Moscow
	cases := []struct {
		name    string
		subject string
		due     time.Time
		want    string
	}{
		{"subject and due", "Матанализ", due, "Матанализ: домашнее задание к 27.10"},
		{"due only", "", due, "Домашнее задание к 27.10"},
		{"subject only", "Матанализ", time.Time{}, "Матанализ: домашнее задание"},
		{"neither", "", time.Time{}, "Домашнее задание"},
		{"header injection", "Физика\r\nBcc: evil@example.com", time.Time{}, "Физика Bcc: evil@example.com: домашнее задание"},
		{"only control characters", "\r\n\u202e", time.Time{}, "Домашнее задание"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HomeworkEmailSubject(tc.subject, tc.due); got != tc.want {
				t.Fatalf("subject = %q, want %q", got, tc.want)
			}
		})
	}

	got := HomeworkEmailSubject(strings.Repeat("я", 1000), due)
	if n := utf8.RuneCountInString(got); n != MaxEmailSubjectRunes || !strings.HasSuffix(got, ": домашнее задание к 27.10") {
		t.Fatalf("long subject: %d runes, %q", n, got)
	}
}

func TestResolveScheduledLesson(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	start := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	recorded := start.Add(2 * time.Minute)
	snap := ScheduledLesson{Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов", Start: start}
	live := ScheduledLesson{Subject: "География", Kind: "seminar", Teacher: "Сидоров", Start: start.Add(time.Hour)}
	with := func(l ScheduledLesson, edit func(*ScheduledLesson)) *ScheduledLesson {
		edit(&l)
		return &l
	}

	cases := []struct {
		name           string
		snapshot, live *ScheduledLesson
		recorded       time.Time
		zone           *time.Location
		want           ScheduledLesson
	}{
		{"snapshot wins", &snap, &live, recorded, msk, ScheduledLesson{"Матанализ", "lecture", "Иванов", start.In(msk)}},
		{"live without snapshot", nil, &live, recorded, msk,
			ScheduledLesson{"География", "seminar", "Сидоров", start.Add(time.Hour).In(msk)}},
		{"unknown live kind", nil, with(live, func(l *ScheduledLesson) { l.Kind = UnknownLessonKind }), recorded, msk,
			ScheduledLesson{"География", "", "Сидоров", start.Add(time.Hour).In(msk)}},
		{"snapshot kind kept as is", with(snap, func(l *ScheduledLesson) { l.Kind = UnknownLessonKind }), nil, recorded, msk,
			ScheduledLesson{"Матанализ", UnknownLessonKind, "Иванов", start.In(msk)}},
		{"snapshot without start", with(snap, func(l *ScheduledLesson) { l.Start = time.Time{} }), &live, recorded, msk,
			ScheduledLesson{"Матанализ", "lecture", "Иванов", recorded.In(msk)}},
		{"nothing but the recording", nil, nil, recorded, msk, ScheduledLesson{Start: recorded.In(msk)}},
		{"nothing at all", nil, nil, time.Time{}, msk, ScheduledLesson{}},
		{"no zone", nil, &live, recorded, nil, live},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveScheduledLesson(tc.snapshot, tc.live, tc.recorded, tc.zone)
			if got != tc.want || got.Start.Location() != tc.want.Start.Location() {
				t.Fatalf("got %+v (%s), want %+v (%s)", got, got.Start.Location(), tc.want, tc.want.Start.Location())
			}
		})
	}
}
