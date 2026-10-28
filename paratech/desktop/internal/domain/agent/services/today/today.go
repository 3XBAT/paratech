package today

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Limits of the rules.
const (
	BurningDays     = 7  // «Горит на 7 дней»: due today or within the next 7 calendar days
	UrgentDays      = 2  // a deadline at most this many days away is marked urgent
	MaxChips        = 4  // hint chips under «Спросить paratech»
	MaxSubjectRunes = 60 // a subject name in a chip text
)

// Texts of the cards and chips (impersonal voice, ADR-0021).
const (
	textSummaryReady  = "Конспект готов"
	textHomeworkFound = "Найдено ДЗ"
	textHomeworkDue   = "Найдено ДЗ до "
	textNoHomework    = "ДЗ не найдено"
	textSeparator     = " · "
	dateLayout        = "02.01"

	chipBurningText   = "Что горит на неделе?"
	chipFileText      = "Конспект по файлу"
	chipYesterdayText = "Повторить вчерашнюю пару"
	chipSubjectText   = "Объясни пару: "
)

// DayStart returns the start of the calendar day of now in its location.
func DayStart(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// DaysFrom returns the calendar days from the day of now to the day of t, both read in now's location: 0 is today,
// 1 tomorrow, -1 yesterday. Clock changes do not shift it.
func DaysFrom(now, t time.Time) int {
	day := func(x time.Time) time.Time {
		y, m, d := x.In(now.Location()).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(day(t).Sub(day(now)) / (24 * time.Hour))
}

// Deadline is an assignment with its deadline as the student sees it. Title is model output shown as data.
type Deadline struct {
	Assignment shared.AssignmentID
	Title      string
	Subject    string
	Due        time.Time // zero: no deadline
	Done       bool      // marked «Выполнено» (ADR-0033 §9): it burns no more
}

// Burning is a deadline of «Горит на 7 дней».
type Burning struct {
	Deadline
	DaysLeft int  // calendar days from today, 0..BurningDays
	Urgent   bool // at most UrgentDays away
}

// BurningOf returns the deadlines not yet passed that fall within BurningDays calendar days, soonest first; a done
// assignment never burns.
func BurningOf(ds []Deadline, now time.Time) []Burning {
	out := []Burning{}
	for _, d := range ds {
		if d.Done || d.Due.IsZero() || d.Due.Before(now) {
			continue
		}
		left := DaysFrom(now, d.Due)
		if left > BurningDays {
			continue
		}
		out = append(out, Burning{Deadline: d, DaysLeft: left, Urgent: left <= UrgentDays})
	}
	slices.SortFunc(out, func(a, b Burning) int {
		return cmp.Or(a.Due.Compare(b.Due), cmp.Compare(a.Assignment, b.Assignment))
	})
	return out
}

// Homework is an assignment found in the transcript of a post.
type Homework struct {
	Assignment shared.AssignmentID
	Due        time.Time // zero: no deadline
	Done       bool      // marked «Выполнено» (ADR-0033 §9)
}

// PostFacts are what is known of a post when it is shown.
type PostFacts struct {
	Summary         bool       // the summary is ready
	HomeworkChecked bool       // the homework was looked for
	Homework        []Homework // the assignments of the transcript now
}

// Card is a post as «Сегодня» and the subject thread show it: «Конспект готов · Найдено ДЗ до 24.10 · [Да, в
// календарь] [Открыть]».
type Card struct {
	Headline    string
	Summary     bool      // «Открыть» opens the summary
	Homework    int       // assignments found, done ones too: the post is the history of the lesson
	Done        int       // of them marked «Выполнено» (ADR-0033 §9)
	EarliestDue time.Time // zero: none of the open ones has a deadline
	// Calendar are the open assignments whose deadline is still ahead, soonest first: «Да, в календарь» proposes them.
	// It only names them; the proposal is checked and confirmed on the existing path (hard rule 3).
	Calendar []Homework
}

// CardOf builds the card of a post; dates are written in now's location. A done assignment is counted (Homework and
// Done) but gives no deadline and no calendar hint: it burns no more.
func CardOf(f PostFacts, now time.Time) Card {
	c := Card{Summary: f.Summary, Homework: len(f.Homework), Calendar: []Homework{}}
	for _, h := range f.Homework {
		if h.Done {
			c.Done++
			continue
		}
		if h.Due.IsZero() {
			continue
		}
		if c.EarliestDue.IsZero() || h.Due.Before(c.EarliestDue) {
			c.EarliestDue = h.Due
		}
		if h.Due.After(now) {
			c.Calendar = append(c.Calendar, h)
		}
	}
	slices.SortFunc(c.Calendar, func(a, b Homework) int {
		return cmp.Or(a.Due.Compare(b.Due), cmp.Compare(a.Assignment, b.Assignment))
	})

	var parts []string
	if f.Summary {
		parts = append(parts, textSummaryReady)
	}
	switch {
	case c.Homework > 0 && !c.EarliestDue.IsZero():
		parts = append(parts, textHomeworkDue+c.EarliestDue.In(now.Location()).Format(dateLayout))
	case c.Homework > 0:
		parts = append(parts, textHomeworkFound)
	case f.HomeworkChecked && f.Summary:
		parts = append(parts, textNoHomework)
	}
	c.Headline = strings.Join(parts, textSeparator)
	return c
}

// ChipKind is the kind of a hint chip.
type ChipKind string

// Chip kinds.
const (
	ChipBurning   ChipKind = "burning"          // «Что горит на неделе?»
	ChipSubject   ChipKind = "explain_subject"  // «Объясни пару: <предмет>»
	ChipYesterday ChipKind = "repeat_yesterday" // «Повторить вчерашнюю пару»
	ChipFile      ChipKind = "file_summary"     // «Конспект по файлу»
)

// Chip is a hint under «Спросить paratech»; Text is what a click puts into the field.
type Chip struct {
	Kind ChipKind
	Text string
}

// ChipFacts are what the chips depend on.
type ChipFacts struct {
	Burning          int    // deadlines in «Горит на 7 дней»
	TodaySubject     string // subject of the newest lesson with a summary ready today; "" none
	SummaryYesterday bool   // a lesson of yesterday has a summary
}

// Chips returns the hint chips, at most MaxChips: what burns, the lesson of today, yesterday's lesson, then a summary
// of a file, which is always offered.
func Chips(f ChipFacts) []Chip {
	out := make([]Chip, 0, MaxChips)
	if f.Burning > 0 {
		out = append(out, Chip{Kind: ChipBurning, Text: chipBurningText})
	}
	if s := shortSubject(f.TodaySubject); s != "" {
		out = append(out, Chip{Kind: ChipSubject, Text: chipSubjectText + s})
	}
	if f.SummaryYesterday {
		out = append(out, Chip{Kind: ChipYesterday, Text: chipYesterdayText})
	}
	return append(out, Chip{Kind: ChipFile, Text: chipFileText})
}

// shortSubject is the subject on one line, cut to MaxSubjectRunes.
func shortSubject(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxSubjectRunes {
		s = strings.TrimSpace(string(r[:MaxSubjectRunes])) + "…"
	}
	return s
}
