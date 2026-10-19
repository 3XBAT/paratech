package csvschedule

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	// The zone database is compiled in: Windows has no IANA database and the app must not depend on a Go install.
	_ "time/tzdata"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// MaxFileBytes bounds the file; a semester of lessons is a few hundred kilobytes at most.
const MaxFileBytes = 2 << 20

// Column names of the header.
const (
	colTitle    = "title"
	colDate     = "date"
	colStart    = "start"
	colEnd      = "end"
	colTimeZone = "timezone"
	colUID      = "uid"
	colSubject  = "subject"
	colKind     = "kind"
	colTeacher  = "teacher"
)

// columnAliases are the other accepted names of the optional columns.
var columnAliases = map[string]string{"предмет": colSubject, "тип": colKind, "преподаватель": colTeacher}

// optionalColumns are -1 in the column map when absent.
var optionalColumns = []string{colUID, colSubject, colKind, colTeacher}

var requiredColumns = []string{colTitle, colDate, colStart, colEnd, colTimeZone}

const (
	dateLayout   = "2006-01-02"
	clockLayout  = "15:04"
	cancelCheck  = 256 // rows between context checks
	keyAutoStart = "auto:"
	keyIDStart   = "id:"
)

var bom = []byte{0xEF, 0xBB, 0xBF} // UTF-8 BOM, written by Excel "CSV UTF-8"

// Parser reads the CSV schedule format of the package comment. The zero value is ready to use.
type Parser struct{}

// New returns a CSV schedule parser.
func New() *Parser { return &Parser{} }

// Parse implements the schedulesync.ScheduleParser port.
func (p *Parser) Parse(ctx context.Context, data []byte) (importplan.Parsed, error) {
	if len(data) > MaxFileBytes {
		return importplan.Parsed{}, &Error{Kind: ErrTooLarge, Detail: fmt.Sprintf("more than %d bytes", MaxFileBytes)}
	}
	data = bytes.TrimPrefix(data, bom)
	if !utf8.Valid(data) {
		return importplan.Parsed{}, &Error{Kind: ErrEncoding}
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = separator(data)
	r.FieldsPerRecord = -1 // a short row is a row error, not the end of the file

	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return importplan.Parsed{}, &Error{Kind: ErrHeader, Detail: "the file is empty"}
	}
	if err != nil {
		return importplan.Parsed{}, malformed(err)
	}
	cols, err := columns(header)
	if err != nil {
		return importplan.Parsed{}, err
	}

	var out importplan.Parsed
	zones := map[string]*time.Location{}
	for n := 0; ; n++ {
		if n%cancelCheck == 0 {
			if err := ctx.Err(); err != nil {
				return importplan.Parsed{}, err
			}
		}
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return importplan.Parsed{}, malformed(err)
		}
		if n >= importplan.MaxRows {
			return importplan.Parsed{}, &Error{Kind: ErrTooMany, Detail: fmt.Sprintf("more than %d lessons", importplan.MaxRows)}
		}

		line, _ := r.FieldPos(0)
		if len(rec) != len(header) {
			out.Errors = append(out.Errors, importplan.RowError{
				Line: line, Reason: fmt.Sprintf("%d fields, the header has %d", len(rec), len(header)),
			})
			continue
		}
		row, rowErr := parseRow(line, rec, cols, zones)
		if rowErr != nil {
			out.Errors = append(out.Errors, *rowErr)
			continue
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// separator picks ';' only when the header line has semicolons and no commas.
func separator(data []byte) rune {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.IndexByte(first, ';') >= 0 && bytes.IndexByte(first, ',') < 0 {
		return ';'
	}
	return ','
}

func malformed(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return &Error{Kind: ErrMalformed, Line: pe.Line, Detail: pe.Err.Error()}
	}
	return &Error{Kind: ErrMalformed, Detail: err.Error()}
}

// columns maps a column name to its index; an optional column is -1 when absent.
func columns(header []string) (map[string]int, error) {
	cols := map[string]int{}
	for _, c := range optionalColumns {
		cols[c] = -1
	}
	seen := map[string]bool{}
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		if alias, ok := columnAliases[name]; ok {
			name = alias
		}
		switch name {
		case colTitle, colDate, colStart, colEnd, colTimeZone, colUID, colSubject, colKind, colTeacher:
		default:
			continue // unknown columns are ignored
		}
		if seen[name] {
			return nil, &Error{Kind: ErrHeader, Line: 1, Detail: fmt.Sprintf("column %q appears twice", name)}
		}
		seen[name] = true
		cols[name] = i
	}

	var missing []string
	for _, c := range requiredColumns {
		if !seen[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return nil, &Error{Kind: ErrHeader, Line: 1, Detail: "missing columns: " + strings.Join(missing, ", ")}
	}
	return cols, nil
}

func parseRow(line int, rec []string, cols map[string]int, zones map[string]*time.Location) (importplan.Row, *importplan.RowError) {
	get := func(c string) string {
		if cols[c] < 0 {
			return ""
		}
		return strings.TrimSpace(rec[cols[c]])
	}
	fail := func(field, reason string) (importplan.Row, *importplan.RowError) {
		return importplan.Row{}, &importplan.RowError{Line: line, Field: field, Reason: reason}
	}

	title := get(colTitle)
	if title == "" {
		return fail(colTitle, "empty title")
	}
	zone, err := loadZone(get(colTimeZone), zones)
	if err != nil {
		return fail(colTimeZone, "not a known IANA time zone (for example Europe/Moscow)")
	}
	date, err := time.Parse(dateLayout, get(colDate))
	if err != nil {
		return fail(colDate, "not a date in the form YYYY-MM-DD")
	}
	start, ok := clockOn(date, get(colStart), zone)
	if !ok {
		return fail(colStart, "not a time in the form HH:MM that exists on that date")
	}
	end, ok := clockOn(date, get(colEnd), zone)
	if !ok {
		return fail(colEnd, "not a time in the form HH:MM that exists on that date")
	}
	if !end.After(start) {
		return fail(colEnd, "end is not after start")
	}

	// Empty subject or kind cells, or no such columns: the title tells (lesson.ClassifyTitle).
	subject, kind := lesson.ClassifyTitle(title)
	if s := get(colSubject); s != "" {
		subject = lesson.CleanText(s, lesson.MaxSubjectRunes)
	}
	if k := get(colKind); k != "" {
		if kind, ok = lesson.ParseKind(k); !ok {
			return fail(colKind, "not a lesson type (lecture, seminar, practice or lab, in English or Russian)")
		}
	}
	teacher := lesson.CleanText(get(colTeacher), lesson.MaxTeacherRunes)

	key := keyAutoStart + date.Format(dateLayout) + " " + start.Format(clockLayout) + " " + title
	if get(colUID) != "" {
		key = keyIDStart + get(colUID)
	}
	return importplan.Row{
		Line: line, Key: key,
		Content: lesson.Content{
			Title: title, Start: start, End: end, TimeZone: zone.String(), Subject: subject, Kind: kind, Teacher: teacher,
		},
	}, nil
}

// loadZone resolves an IANA name. "" and "Local" are refused: LoadLocation maps them to UTC and to the machine zone.
func loadZone(name string, cache map[string]*time.Location) (*time.Location, error) {
	if loc, ok := cache[name]; ok {
		return loc, nil
	}
	if name == "Local" || lesson.ValidateTimeZoneName(name) != nil {
		return nil, errors.New("bad zone name")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	cache[name] = loc
	return loc, nil
}

// clockOn places HH:MM on the date in loc. A time skipped by a daylight saving change does not exist and is refused
// (time.Date would silently move it).
func clockOn(date time.Time, hhmm string, loc *time.Location) (time.Time, bool) {
	c, err := time.Parse(clockLayout, hhmm)
	if err != nil {
		return time.Time{}, false
	}
	t := time.Date(date.Year(), date.Month(), date.Day(), c.Hour(), c.Minute(), 0, 0, loc)
	if t.Hour() != c.Hour() || t.Minute() != c.Minute() || t.Day() != date.Day() {
		return time.Time{}, false
	}
	return t, true
}
