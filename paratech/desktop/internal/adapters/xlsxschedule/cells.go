package xlsxschedule

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	// The zone database is compiled in: Windows has no IANA database and the app must not depend on a Go install.
	_ "time/tzdata"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Column names of the header, as in the CSV format. Adapters do not import each other (ARCH-30), so the small
// mapping of csvschedule is repeated here; the identity keys must stay the same.
const (
	colTitle    = "title"
	colDate     = "date"
	colStart    = "start"
	colEnd      = "end"
	colTimeZone = "timezone"
	colUID      = "uid"
)

var requiredColumns = []string{colTitle, colDate, colStart, colEnd, colTimeZone}

const (
	dateLayout   = "2006-01-02"
	clockLayout  = "15:04"
	keyAutoStart = "auto:"
	keyIDStart   = "id:"
)

// Excel date serials: day 0 of the 1900 system is 1899-12-30 for every serial from 61 (1900-03-01) on; serials below
// count the fictitious 1900-02-29 and are no lesson dates anyway. The last valid serial is 9999-12-31.
const (
	minSerial1900 = 61
	maxSerial1900 = 2958465
	maxSerial1904 = maxSerial1900 - 1462
	secondsPerDay = 24 * 60 * 60
)

var (
	epoch1900 = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	epoch1904 = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
)

// rowReader turns the cells of one data row into a lesson row.
type rowReader struct {
	cols     map[string]int // column name -> cell index; uid is -1 when absent
	date1904 bool
	zones    map[string]*time.Location
}

func newRowReader(line int, header []string, date1904 bool) (*rowReader, error) {
	cols := map[string]int{colUID: -1}
	seen := map[string]bool{}
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		switch name {
		case colTitle, colDate, colStart, colEnd, colTimeZone, colUID:
		default:
			continue // unknown columns are ignored
		}
		if seen[name] {
			return nil, &Error{Kind: ErrHeader, Line: line, Detail: fmt.Sprintf("column %q appears twice", name)}
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
		return nil, &Error{Kind: ErrHeader, Line: line, Detail: "missing columns: " + strings.Join(missing, ", ")}
	}
	return &rowReader{cols: cols, date1904: date1904, zones: map[string]*time.Location{}}, nil
}

// fail is a row error of one column. Field is the bare column name, as in CSV and the domain; the reason starts with
// the column letter as Excel shows it.
func (r *rowReader) fail(line int, col, reason string) (importplan.Row, *importplan.RowError) {
	return importplan.Row{}, &importplan.RowError{
		Line: line, Field: col, Reason: "column " + columnLetters(r.cols[col]) + ": " + reason,
	}
}

func (r *rowReader) parse(line int, cells []string) (importplan.Row, *importplan.RowError) {
	get := func(c string) string {
		i := r.cols[c]
		if i < 0 || i >= len(cells) {
			return "" // excelize drops empty cells at the end of a row
		}
		return strings.TrimSpace(cells[i])
	}

	// The domain checks run here too so that each problem names its column; importplan would reject the same rows
	// later without one.
	title := get(colTitle)
	if err := lesson.ValidateTitle(title); err != nil {
		return r.fail(line, colTitle, domainReason(err))
	}
	zone, err := loadZone(get(colTimeZone), r.zones)
	if err != nil {
		return r.fail(line, colTimeZone, "not a known IANA time zone (for example Europe/Moscow)")
	}
	date, ok := r.date(get(colDate))
	if !ok {
		return r.fail(line, colDate, "not a date cell or a date in the form YYYY-MM-DD")
	}
	start, ok := clockOn(date, get(colStart), zone)
	if !ok {
		return r.fail(line, colStart, "not a time cell or a time in the form HH:MM that exists on that date")
	}
	end, ok := clockOn(date, get(colEnd), zone)
	if !ok {
		return r.fail(line, colEnd, "not a time cell or a time in the form HH:MM that exists on that date")
	}
	if err := lesson.ValidateSpan(start, end); err != nil {
		return r.fail(line, colEnd, domainReason(err))
	}

	key, keyCol := keyAutoStart+date.Format(dateLayout)+" "+start.Format(clockLayout)+" "+title, colTitle
	if uid := get(colUID); r.cols[colUID] >= 0 && uid != "" {
		key, keyCol = keyIDStart+uid, colUID
	}

	if err := lesson.ValidateSourceUID(importplan.SourceXLSX.UID(key)); err != nil {
		if keyCol == colTitle { // the automatic identity contains the title
			return r.fail(line, colTitle, "title too long for the lesson identity; add a uid column")
		}
		return r.fail(line, colUID, domainReason(err))
	}
	return importplan.Row{
		Line: line, Key: key,
		Content: lesson.Content{Title: title, Start: start, End: end, TimeZone: zone.String()},
	}, nil
}

// domainReason strips the sentinel prefix, as importplan does: "title longer than 512 bytes".
func domainReason(err error) string {
	return strings.TrimPrefix(err.Error(), lesson.ErrInvalidLesson.Error()+": ")
}

// date reads YYYY-MM-DD text or a whole-day date serial; the result is midnight UTC of that civil date.
func (r *rowReader) date(s string) (time.Time, bool) {
	if d, err := time.Parse(dateLayout, s); err == nil {
		return d, true
	}
	v, ok := serial(s)
	if !ok || v != math.Trunc(v) {
		return time.Time{}, false
	}
	epoch, lo, hi := epoch1900, float64(minSerial1900), float64(maxSerial1900)
	if r.date1904 {
		epoch, lo, hi = epoch1904, 0, maxSerial1904
	}
	if v < lo || v > hi {
		return time.Time{}, false
	}
	return epoch.AddDate(0, 0, int(v)), true
}

// clockOf reads HH:MM text or a time serial (a fraction of a day that is a whole minute).
func clockOf(s string) (hour, minute int, ok bool) {
	if c, err := time.Parse(clockLayout, s); err == nil {
		return c.Hour(), c.Minute(), true
	}
	v, ok := serial(s)
	if !ok || v < 0 || v >= 1 {
		return 0, 0, false
	}
	// Times are stored as binary fractions (09:30 is 0.3958333...); rounding to the second recovers the time typed.
	sec := int(math.Round(v * secondsPerDay))
	if sec >= secondsPerDay || sec%60 != 0 {
		return 0, 0, false
	}
	return sec / 3600, sec % 3600 / 60, true
}

// serial parses a stored number. Only decimal notation is accepted (no hex floats, Inf or NaN).
func serial(s string) (float64, bool) {
	if s == "" || strings.Trim(s, "0123456789.eE+-") != "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

// clockOn places the time of day on the date in loc. A time skipped by a daylight saving change does not exist and is
// refused (time.Date would silently move it).
func clockOn(date time.Time, s string, loc *time.Location) (time.Time, bool) {
	h, m, ok := clockOf(s)
	if !ok {
		return time.Time{}, false
	}
	t := time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, loc)
	if t.Hour() != h || t.Minute() != m || t.Day() != date.Day() {
		return time.Time{}, false
	}
	return t, true
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

// columnLetters returns the Excel name of a zero-based column index (0 is A, 26 is AA).
func columnLetters(i int) string {
	var b []byte
	for n := i + 1; n > 0; n = (n - 1) / 26 {
		b = append([]byte{byte('A' + (n-1)%26)}, b...)
	}
	return string(b)
}
