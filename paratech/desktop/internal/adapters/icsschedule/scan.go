package icsschedule

import (
	"bytes"
	"fmt"

	ics "github.com/arran4/golang-ical"
)

// Limits of one content line, checked before golang-ical sees the file: its parameter parser copies the rest of the
// line for every parameter value, so a long line with many parameters would take quadratic time.
const (
	maxLineBytes       = 64 << 10
	maxParamSeparators = 32 // ';' and ',' before the value; real lines have fewer than ten
	maxDepth           = 16 // nested components; real files have 3 (VCALENDAR, VEVENT, VALARM). golang-ical recurses.
)

const (
	tokenBegin   = "BEGIN"
	tokenEnd     = "END"
	compCalendar = "VCALENDAR"
	compEvent    = "VEVENT"
)

// layout is what the pre-scan learned about the file: golang-ical keeps no line numbers.
type layout struct {
	// events holds the physical line of each BEGIN:VEVENT directly inside the VCALENDAR, in file order. It matches
	// Calendar.Events() one to one; Parse checks the count.
	events []int

	// logical holds the physical line where each non-empty logical (unfolded) line starts; golang-ical reports
	// logical line numbers in its errors.
	logical []int
}

// physical maps a logical line number of a golang-ical error to a line of the file, or 0 when unknown.
func (l layout) physical(logicalLine int) int {
	if logicalLine < 1 || logicalLine > len(l.logical) {
		return 0
	}
	return l.logical[logicalLine-1]
}

// scan unfolds the content lines like golang-ical does (a line starting with a space or a tab continues the previous
// one; CRLF and LF both end a line), enforces the line limits and tracks BEGIN/END to find the events.
func scan(data []byte) (layout, error) {
	var (
		out       layout
		cur       []byte
		curStart  int
		depth     int
		opened    bool
		closed    bool
		lineCount int
	)

	finish := func() error {
		if len(cur) == 0 {
			return nil
		}
		out.logical = append(out.logical, curStart)
		if n := paramSeparators(cur); n > maxParamSeparators {
			return &Error{Kind: ErrMalformed, Line: curStart, Detail: fmt.Sprintf("more than %d parameters", maxParamSeparators)}
		}
		token, value, ok := beginEnd(cur)
		if !ok {
			return nil
		}
		switch token {
		case tokenBegin:
			if depth == 0 {
				if opened || value != compCalendar {
					return &Error{Kind: ErrNotCalendar, Line: curStart, Detail: "expected one VCALENDAR"}
				}
				opened = true
			}
			if depth == 1 && value == compEvent {
				out.events = append(out.events, curStart)
				if len(out.events) > MaxEvents {
					return &Error{Kind: ErrTooMany, Detail: fmt.Sprintf("more than %d events", MaxEvents)}
				}
			}
			depth++
			if depth > maxDepth {
				return &Error{Kind: ErrMalformed, Line: curStart, Detail: fmt.Sprintf("components nested deeper than %d", maxDepth)}
			}
		case tokenEnd:
			depth--
			if depth == 0 && value == compCalendar {
				closed = true
			}
		}
		return nil
	}

	for len(data) > 0 {
		lineCount++
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
			line = bytes.TrimSuffix(line, []byte{'\r'})
		} else {
			data = nil
		}

		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(cur) == 0 {
				curStart = lineCount
			}
			cur = append(cur, line[1:]...)
		} else {
			if err := finish(); err != nil {
				return layout{}, err
			}
			cur, curStart = append(cur[:0], line...), lineCount
		}
		if len(cur) > maxLineBytes {
			return layout{}, &Error{Kind: ErrMalformed, Line: curStart, Detail: fmt.Sprintf("a line longer than %d bytes", maxLineBytes)}
		}
	}
	if err := finish(); err != nil {
		return layout{}, err
	}

	if !opened || !closed {
		return layout{}, &Error{Kind: ErrNotCalendar, Detail: "no complete BEGIN:VCALENDAR ... END:VCALENDAR"}
	}
	return out, nil
}

// paramSeparators counts ';' and ',' before the first ':' outside a quoted parameter value.
func paramSeparators(line []byte) int {
	n := 0
	quoted := false
	for _, b := range line {
		switch {
		case b == '"':
			quoted = !quoted
		case quoted:
		case b == ':':
			return n
		case b == ';' || b == ',':
			n++
		}
	}
	return n
}

// beginEnd reads a BEGIN or END line with golang-ical's own property parser, so the pre-scan sees the same
// structure as the library (which, for example, does not anchor the property name).
func beginEnd(line []byte) (token, value string, ok bool) {
	if !bytes.Contains(line, []byte(tokenBegin)) && !bytes.Contains(line, []byte(tokenEnd)) {
		return "", "", false
	}
	p, err := ics.ParseProperty(ics.ContentLine(line))
	if err != nil || p == nil {
		return "", "", false // golang-ical refuses the line too and Parse reports it
	}
	if p.IANAToken != tokenBegin && p.IANAToken != tokenEnd {
		return "", "", false
	}
	return p.IANAToken, p.Value, true // compared exactly, as golang-ical does
}
