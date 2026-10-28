package today

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var (
	msk = time.FixedZone("MSK", 3*60*60)
	// Wednesday, 21 October 2026, 12:20 in the student's zone.
	now = time.Date(2026, 10, 21, 12, 20, 0, 0, msk)
)

func at(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, msk) }

func TestDaysFrom(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want int
	}{
		{"later today", at(21, 23), 0},
		{"earlier today", at(21, 1), 0},
		{"tomorrow just after midnight", at(22, 0), 1},
		{"yesterday", at(20, 23), -1},
		{"in a week", at(28, 9), 7},
		{"other zone, same local day", time.Date(2026, 10, 21, 20, 30, 0, 0, time.UTC), 0},
		{"other zone, next local day", time.Date(2026, 10, 21, 21, 30, 0, 0, time.UTC), 1},
	}
	for _, tc := range cases {
		if got := DaysFrom(now, tc.t); got != tc.want {
			t.Errorf("%s: DaysFrom = %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := DayStart(now); !got.Equal(at(21, 0)) {
		t.Errorf("DayStart = %v", got)
	}
}

func TestBurningOf(t *testing.T) {
	ds := []Deadline{
		{Assignment: "late", Due: at(28, 23)},             // day 7: in
		{Assignment: "too-late", Due: at(29, 0)},          // day 8: out
		{Assignment: "passed", Due: at(21, 9)},            // earlier today: out
		{Assignment: "none"},                              // no deadline: out
		{Assignment: "soon", Due: at(23, 23)},             // 2 days: urgent
		{Assignment: "tonight", Due: at(21, 23)},          // today: urgent
		{Assignment: "saturday", Due: at(24, 23)},         // 3 days
		{Assignment: "done", Due: at(22, 23), Done: true}, // marked «Выполнено»: out
	}
	got := BurningOf(ds, now)
	var ids []shared.AssignmentID
	for _, b := range got {
		ids = append(ids, b.Assignment)
	}
	want := []shared.AssignmentID{"tonight", "soon", "saturday", "late"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("burning = %v, want %v", ids, want)
	}
	wantDays := []int{0, 2, 3, 7}
	wantUrgent := []bool{true, true, false, false}
	for i, b := range got {
		if b.DaysLeft != wantDays[i] || b.Urgent != wantUrgent[i] {
			t.Errorf("%s: days %d urgent %v, want %d %v", b.Assignment, b.DaysLeft, b.Urgent, wantDays[i], wantUrgent[i])
		}
	}
	if got := BurningOf(nil, now); got == nil || len(got) != 0 {
		t.Fatalf("BurningOf(nil) = %#v, want an empty list", got)
	}
}

func TestCardOf(t *testing.T) {
	cases := []struct {
		name      string
		facts     PostFacts
		headline  string
		homework  int
		done      int
		calendar  []shared.AssignmentID
		earliest  time.Time
		summaryOK bool
	}{
		{
			name:      "summary and homework with deadlines",
			facts:     PostFacts{Summary: true, HomeworkChecked: true, Homework: []Homework{{Assignment: "a2", Due: at(27, 23)}, {Assignment: "a1", Due: at(24, 23)}, {Assignment: "a0", Due: at(20, 9)}}},
			headline:  "Конспект готов · Найдено ДЗ до 20.10",
			homework:  3,
			calendar:  []shared.AssignmentID{"a1", "a2"},
			earliest:  at(20, 9),
			summaryOK: true,
		},
		{
			name:     "homework first, no summary yet",
			facts:    PostFacts{HomeworkChecked: true, Homework: []Homework{{Assignment: "a1", Due: at(24, 23)}}},
			headline: "Найдено ДЗ до 24.10", homework: 1, calendar: []shared.AssignmentID{"a1"}, earliest: at(24, 23),
		},
		{
			name: "a done assignment is counted, gives no deadline and no calendar hint",
			facts: PostFacts{Summary: true, HomeworkChecked: true, Homework: []Homework{
				{Assignment: "a1", Due: at(22, 23), Done: true}, {Assignment: "a2", Due: at(27, 23)},
			}},
			headline: "Конспект готов · Найдено ДЗ до 27.10", homework: 2, done: 1, calendar: []shared.AssignmentID{"a2"},
			earliest: at(27, 23), summaryOK: true,
		},
		{
			name:     "every assignment done",
			facts:    PostFacts{Summary: true, HomeworkChecked: true, Homework: []Homework{{Assignment: "a1", Due: at(22, 23), Done: true}}},
			headline: "Конспект готов · Найдено ДЗ", homework: 1, done: 1, summaryOK: true,
		},
		{
			name:     "homework without a deadline",
			facts:    PostFacts{Summary: true, HomeworkChecked: true, Homework: []Homework{{Assignment: "a1"}}},
			headline: "Конспект готов · Найдено ДЗ", homework: 1, summaryOK: true,
		},
		{
			name:     "nothing found",
			facts:    PostFacts{Summary: true, HomeworkChecked: true},
			headline: "Конспект готов · ДЗ не найдено", summaryOK: true,
		},
		{
			name:     "summary, homework not looked for yet",
			facts:    PostFacts{Summary: true},
			headline: "Конспект готов", summaryOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := CardOf(tc.facts, now)
			var cal []shared.AssignmentID
			for _, h := range c.Calendar {
				cal = append(cal, h.Assignment)
			}
			if c.Headline != tc.headline || c.Homework != tc.homework || c.Done != tc.done || !reflect.DeepEqual(cal, tc.calendar) ||
				!c.EarliestDue.Equal(tc.earliest) || c.Summary != tc.summaryOK {
				t.Fatalf("card = %+v", c)
			}
			if strings.Contains(c.Headline, "я ") || c.Calendar == nil {
				t.Fatalf("card = %+v: impersonal text and a non-nil calendar list expected", c)
			}
		})
	}
}

func TestChips(t *testing.T) {
	long := strings.Repeat("Ж", MaxSubjectRunes+5)
	cases := []struct {
		name  string
		facts ChipFacts
		want  []ChipKind
		texts map[ChipKind]string
	}{
		{"nothing known: only the file chip", ChipFacts{}, []ChipKind{ChipFile}, nil},
		{
			name:  "everything",
			facts: ChipFacts{Burning: 2, TodaySubject: " Математический\nанализ ", SummaryYesterday: true},
			want:  []ChipKind{ChipBurning, ChipSubject, ChipYesterday, ChipFile},
			texts: map[ChipKind]string{
				ChipBurning: "Что горит на неделе?", ChipSubject: "Объясни пару: Математический анализ",
				ChipYesterday: "Повторить вчерашнюю пару", ChipFile: "Конспект по файлу",
			},
		},
		{
			name: "long subject is cut", facts: ChipFacts{TodaySubject: long}, want: []ChipKind{ChipSubject, ChipFile},
			texts: map[ChipKind]string{ChipSubject: "Объясни пару: " + strings.Repeat("Ж", MaxSubjectRunes) + "…"},
		},
		{"blank subject gives no chip", ChipFacts{TodaySubject: "  "}, []ChipKind{ChipFile}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Chips(tc.facts)
			if len(got) > MaxChips {
				t.Fatalf("%d chips, at most %d", len(got), MaxChips)
			}
			var kinds []ChipKind
			for _, c := range got {
				kinds = append(kinds, c.Kind)
				if want, ok := tc.texts[c.Kind]; ok && c.Text != want {
					t.Errorf("%s text = %q, want %q", c.Kind, c.Text, want)
				}
			}
			if !reflect.DeepEqual(kinds, tc.want) {
				t.Fatalf("chips = %v, want %v", kinds, tc.want)
			}
		})
	}
}
