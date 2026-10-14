package icsschedule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	// The zone database is compiled in: Windows has no IANA database and the app must not depend on a Go install.
	_ "time/tzdata"

	ics "github.com/arran4/golang-ical"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Limits against hostile or broken files.
const (
	// MaxFileBytes bounds the file, as for CSV.
	MaxFileBytes = 2 << 20

	// MaxEvents bounds the VEVENTs of a file, inside the bounds or not.
	MaxEvents = importplan.MaxRows

	// MaxExpansionSteps bounds the recurrence periods walked for the whole file (a day, a week, a month or a year
	// each), so a rule with a huge COUNT cannot keep the parser busy.
	MaxExpansionSteps = 200_000
)

const cancelCheck = 256 // events or expansion steps between context checks

var bom = []byte{0xEF, 0xBB, 0xBF}

// Import dates: occurrences starting in [day start in UTC - WindowPast, + WindowLength) become rows. The day before
// is included so today's lessons are in for every zone ahead of UTC.
const (
	WindowPast   = 24 * time.Hour
	WindowLength = 366 * 24 * time.Hour
)

// Clock supplies the current time for the import dates; the app passes its clock adapter.
type Clock interface {
	Now() time.Time
}

// Parser reads iCalendar schedules (see the package comment). It is safe for concurrent use.
type Parser struct {
	window func() importplan.Window
}

// New returns an ICS parser whose import dates follow the clock.
func New(clock Clock) (*Parser, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: a clock is required", ErrInvalidOptions)
	}
	return &Parser{window: func() importplan.Window { return Window(clock.Now()) }}, nil
}

// Window returns the import dates at now. They change only once a day, so a preview and its commit on the same UTC
// day read the same occurrences.
func Window(now time.Time) importplan.Window {
	from := now.UTC().Truncate(24 * time.Hour).Add(-WindowPast)
	return importplan.Window{From: from, To: from.Add(WindowLength)}
}

// Parse implements the schedulesync.ScheduleParser port.
func (p *Parser) Parse(ctx context.Context, data []byte) (importplan.Parsed, error) {
	if err := ctx.Err(); err != nil {
		return importplan.Parsed{}, err
	}
	if len(data) > MaxFileBytes {
		return importplan.Parsed{}, &Error{Kind: ErrTooLarge, Detail: fmt.Sprintf("more than %d bytes", MaxFileBytes)}
	}
	data = bytes.TrimPrefix(data, bom)
	if !utf8.Valid(data) {
		return importplan.Parsed{}, &Error{Kind: ErrEncoding}
	}

	lay, err := scan(data)
	if err != nil {
		return importplan.Parsed{}, err
	}
	cal, err := parseCalendar(data)
	if err != nil {
		var me *ics.MalformedError
		if errors.As(err, &me) {
			return importplan.Parsed{}, &Error{Kind: ErrMalformed, Line: lay.physical(me.Line), Detail: "not valid iCalendar syntax"}
		}
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "not valid iCalendar syntax"}
	}
	events := cal.Events()
	if len(events) != len(lay.events) {
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "unexpected event structure"}
	}

	w := p.window()
	r := &reader{zones: zones{}, from: w.From, to: w.To}
	r.calZone = calendarZone(cal, r.zones)

	var out importplan.Parsed
	read := make([]event, 0, len(events))
	for i, ev := range events {
		if i%cancelCheck == 0 {
			if err := ctx.Err(); err != nil {
				return importplan.Parsed{}, err
			}
		}
		e, rowErr, err := r.read(ev, lay.events[i])
		if err != nil {
			return importplan.Parsed{}, err
		}
		if rowErr != nil {
			out.Errors = append(out.Errors, *rowErr)
			continue
		}
		read = append(read, e)
	}

	rows, rowErrs, err := occurrences(ctx, w, read)
	if err != nil {
		return importplan.Parsed{}, err
	}
	out.Rows = rows
	out.Window = w
	out.Errors = append(out.Errors, rowErrs...)
	sort.SliceStable(out.Errors, func(i, j int) bool { return out.Errors[i].Line < out.Errors[j].Line })
	return out, nil
}

// parseCalendar runs golang-ical. Properties between components (written by some exporters) are accepted. A panic of
// the library on hostile input becomes a malformed-file error instead of ending the app.
func parseCalendar(data []byte) (cal *ics.Calendar, err error) {
	defer func() {
		if recover() != nil {
			cal, err = nil, errors.New("icsschedule: parser panic")
		}
	}()
	return ics.ParseCalendarWithOptions(bytes.NewReader(data), ics.WithUnknownPropertyHandler(ics.AcceptUnknownPropertyHandler))
}

// calendarZone returns the X-WR-TIMEZONE of the calendar (Google writes it), or nil when absent or unknown.
func calendarZone(cal *ics.Calendar, z zones) *time.Location {
	for _, p := range cal.CalendarProperties {
		if strings.EqualFold(p.IANAToken, propXWRTimezone) {
			if loc, ok := z.resolve(strings.TrimSpace(p.Value)); ok {
				return loc
			}
			return nil
		}
	}
	return nil
}

// occurrences expands the events into rows in file order: the occurrences of a series in time order at the line of
// its master event, a changed occurrence at its own line.
func occurrences(ctx context.Context, w importplan.Window, events []event) ([]importplan.Row, []importplan.RowError, error) {
	var errs []importplan.RowError

	// Series are matched by UID: one master per UID, its changed occurrences by RECURRENCE-ID.
	masters := map[string]*event{}
	overridden := map[string]map[int64]bool{} // uid -> original starts (Unix seconds) replaced or cancelled
	skip := make([]bool, len(events))
	for i := range events {
		e := &events[i]
		if e.uid == "" || e.allDay {
			continue
		}
		if e.recurrenceID == nil {
			if _, dup := masters[e.uid]; dup {
				errs = append(errs, *rowError(e.line, propUID, reasonDuplicateUID))
				skip[i] = true
				continue
			}
			masters[e.uid] = e
			continue
		}
		orig := e.recurrenceID.Unix()
		if overridden[e.uid] == nil {
			overridden[e.uid] = map[int64]bool{}
		}
		if overridden[e.uid][orig] {
			errs = append(errs, *rowError(e.line, propRecurrenceID, reasonDuplicateOccu))
			skip[i] = true
			continue
		}
		overridden[e.uid][orig] = true
	}

	x := expansion{ctx: ctx, budget: new(int)}
	*x.budget = MaxExpansionSteps
	var rows []importplan.Row
	add := func(e *event, k string, start time.Time) error {
		if len(rows) == importplan.MaxRows {
			return &Error{Kind: ErrTooMany, Detail: fmt.Sprintf("more than %d lessons in the import dates", importplan.MaxRows)}
		}
		start = start.In(e.label)
		rows = append(rows, importplan.Row{
			Line: e.line,
			Key:  k,
			Content: lesson.Content{
				Title: e.title, Start: start, End: start.Add(e.duration), TimeZone: e.label.String(),
			},
		})
		return nil
	}

	for i := range events {
		e := &events[i]
		if skip[i] || e.allDay || e.cancelled {
			continue
		}
		if e.uid != "" && e.recurrenceID != nil {
			if m := masters[e.uid]; m != nil && m.cancelled {
				continue // the whole series is cancelled
			}
		}

		if e.recurrenceID != nil {
			if w.Contains(e.start) {
				if err := add(e, key(e.id, *e.recurrenceID), e.start); err != nil {
					return nil, nil, err
				}
			}
			continue
		}

		starts, reason, err := series(x, w, e)
		if err != nil {
			return nil, nil, err
		}
		if reason != "" {
			errs = append(errs, *rowError(e.line, propRRule, reason))
			continue
		}
		replaced := overridden[e.uid]
		if e.uid == "" {
			replaced = nil
		}
		// A one-off event is keyed by its id alone, so moving it shows as a change. Without a UID the id is the
		// title, which needs the start to tell events apart.
		single := e.uid != "" && !e.hasRule && len(e.rdates) == 0 && len(replaced) == 0
		for _, s := range starts {
			if replaced[s.Unix()] {
				continue
			}
			k := e.id
			if !single {
				k = key(e.id, s)
			}
			if err := add(e, k, s); err != nil {
				return nil, nil, err
			}
		}
	}
	return rows, errs, nil
}

// series returns the starts of a master event's occurrences in the bounds, in order: DTSTART or the RRULE
// expansion, plus RDATE, minus EXDATE.
func series(x expansion, w importplan.Window, e *event) ([]time.Time, string, error) {
	var starts []time.Time
	switch {
	case !e.hasRule:
		starts = []time.Time{e.start}
	case e.rule != nil && !e.ruleEnded:
		s, reason, err := x.expand(e.rule, e.start, w.From, w.To)
		if err != nil || reason != "" {
			return nil, reason, err
		}
		starts = s
	}

	loc := e.start.Location()
	seen := make(map[int64]bool, len(starts)+len(e.rdates))
	var out []time.Time
	for i, s := range append(starts, e.rdates...) {
		if i%cancelCheck == 0 {
			if err := x.ctx.Err(); err != nil {
				return nil, "", err
			}
		}
		s = s.In(loc)
		u := s.Unix()
		if seen[u] || !w.Contains(s) || excluded(e, s) {
			continue
		}
		seen[u] = true
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, "", nil
}

// excluded reports whether EXDATE removes the occurrence at s. A date-only EXDATE is a date in the zone the lesson is
// shown in, so for a UTC series of a calendar with X-WR-TIMEZONE it is the local date, not the UTC one.
func excluded(e *event, s time.Time) bool {
	return e.exInstant[s.Unix()] || e.exDay[civilDate(s.In(e.label)).Unix()]
}
