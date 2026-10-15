package xlsxschedule

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// secret is written into hostile cells; no message may quote it.
const secret = "SECRETVALUE"

const sheet1 = "Sheet1"

var header = []any{"title", "date", "start", "end", "timezone", "uid"}

// day is a date cell as Excel stores a typed date (a serial with a date format).
func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// clock is a time cell as Excel stores a typed time (a fraction of a day with a time format).
func clock(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

// workbook writes rows from A1 of the first sheet; edit may change the workbook before it is saved.
func workbook(t *testing.T, rows [][]any, edit func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	for i, r := range rows {
		if err := f.SetSheetRow(sheet1, fmt.Sprintf("A%d", i+1), &r); err != nil {
			t.Fatal(err)
		}
	}
	if edit != nil {
		edit(f)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func parse(t *testing.T, data []byte) importplan.Parsed {
	t.Helper()
	p, err := New().Parse(context.Background(), data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseValid(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	must(t, err)
	data := workbook(t, [][]any{
		{"Title", "Date", "Start", "END", " TimeZone ", "UID", "Room"},
		{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 30), "Europe/Moscow", 101, "A-1"},
		{"Physics, lab", "2026-10-14", "9:45", "11:15", "UTC", nil, 202},
		{"=SUM(A1)", "2026-10-15", clock(12, 0), " 13:00 ", "Europe/Moscow"}, // trailing empty cells are dropped
		{"Chemistry", day(2026, 10, 16), clock(8, 0), nil, "UTC", "  "},
	}, func(f *excelize.File) {
		// A formula gives the value saved with it; nothing is calculated. Excel saves 09:00 for this end time.
		must(t, f.SetCellValue(sheet1, "D5", clock(9, 0)))
		must(t, f.SetCellFormula(sheet1, "D5", "C5+1/24"))
	})

	p := parse(t, data)
	if len(p.Errors) != 0 || len(p.Rows) != 4 {
		t.Fatalf("parsed = %+v", p)
	}
	want := []importplan.Row{
		{Line: 2, Key: "id:101", Content: lesson.Content{Title: "Math",
			Start: time.Date(2026, 10, 14, 9, 0, 0, 0, msk), End: time.Date(2026, 10, 14, 10, 30, 0, 0, msk), TimeZone: "Europe/Moscow"}},
		{Line: 3, Key: "auto:2026-10-14 09:45 Physics, lab", Content: lesson.Content{Title: "Physics, lab",
			Start: time.Date(2026, 10, 14, 9, 45, 0, 0, time.UTC), End: time.Date(2026, 10, 14, 11, 15, 0, 0, time.UTC), TimeZone: "UTC"}},
		{Line: 4, Key: "auto:2026-10-15 12:00 =SUM(A1)", Content: lesson.Content{Title: "=SUM(A1)",
			Start: time.Date(2026, 10, 15, 12, 0, 0, 0, msk), End: time.Date(2026, 10, 15, 13, 0, 0, 0, msk), TimeZone: "Europe/Moscow"}},
		{Line: 5, Key: "auto:2026-10-16 08:00 Chemistry", Content: lesson.Content{Title: "Chemistry",
			Start: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC), TimeZone: "UTC"}},
	}
	for i, w := range want {
		g := p.Rows[i]
		if g.Line != w.Line || g.Key != w.Key || !g.Content.Equal(w.Content) {
			t.Errorf("row %d = %+v, want %+v", i, g, w)
		}
		if err := g.Content.Validate(); err != nil {
			t.Errorf("row %d content invalid: %v", i, err)
		}
	}
}

func TestRowErrors(t *testing.T) {
	ok := []any{"Ok", day(2026, 10, 14), clock(8, 0), clock(8, 30), "UTC"}
	tests := []struct {
		name   string
		row    []any
		edit   func(t *testing.T, f *excelize.File)
		field  string
		letter string
	}{
		{"empty title", []any{" ", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}, nil, "title", "A"},
		{"bad date text", []any{"Math", "14.10." + secret, clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"impossible date", []any{"Math", "2026-02-30", clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"date with a time", []any{"Math", time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"date serial in February 1900", []any{"Math", 59, clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"date serial too large", []any{"Math", 3e6, clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"negative date serial", []any{"Math", -46000, clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"hex number", []any{"Math", "0x1p4", clock(9, 0), clock(10, 0), "UTC"}, nil, "date", "B"},
		{"bad start", []any{"Math", day(2026, 10, 14), "9am" + secret, clock(10, 0), "UTC"}, nil, "start", "C"},
		{"start with seconds", []any{"Math", day(2026, 10, 14), 0.375 + 30.0/secondsPerDay, clock(10, 0), "UTC"}, nil, "start", "C"},
		{"start is a date and time", []any{"Math", day(2026, 10, 14), time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), clock(10, 0), "UTC"}, nil, "start", "C"},
		{"negative start", []any{"Math", day(2026, 10, 14), -0.25, clock(10, 0), "UTC"}, nil, "start", "C"},
		{"infinite start", []any{"Math", day(2026, 10, 14), "Inf", clock(10, 0), "UTC"}, nil, "start", "C"},
		{"start rounds to 24:00", []any{"Math", day(2026, 10, 14), 0.999999999, clock(10, 0), "UTC"}, nil, "start", "C"},
		{"missing end", []any{"Math", day(2026, 10, 14), clock(9, 0), nil, "UTC"}, nil, "end", "D"},
		{"end before start", []any{"Math", day(2026, 10, 14), clock(10, 0), clock(9, 0), "UTC"}, nil, "end", "D"},
		{"unknown zone", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), secret + "/Base"}, nil, "timezone", "E"},
		{"missing zone", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0)}, nil, "timezone", "E"},
		{"local zone", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "Local"}, nil, "timezone", "E"},
		{"number as zone", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), 3}, nil, "timezone", "E"},
		{"dst gap", []any{"Math", day(2026, 3, 29), clock(2, 30), clock(3, 30), "Europe/Berlin"}, nil, "start", "C"},
		{"formula without a saved value", []any{nil, day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"},
			func(t *testing.T, f *excelize.File) { must(t, f.SetCellFormula(sheet1, "A3", `"`+secret+`"`)) }, "title", "A"},
		{"error value", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"},
			func(t *testing.T, f *excelize.File) {
				must(t, f.SetCellValue(sheet1, "C3", "#N/A"))
				must(t, f.SetCellFormula(sheet1, "C3", "NA()"))
			}, "start", "C"},
		{"line break in title", []any{"Math\nGroup 2", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}, nil, "title", "A"},
		{"title over the limit", []any{strings.Repeat("a", lesson.MaxTitleBytes+1), day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}, nil, "title", "A"},
		{"title too long for the automatic identity", []any{strings.Repeat("a", lesson.MaxTitleBytes), day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}, nil, "title", "A"},
		{"uid over the limit", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC", strings.Repeat("u", 600)}, nil, "uid", "F"},
		{"tab in uid", []any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC", "a\tb"}, nil, "uid", "F"},
		// The night the clocks go back has 25 hours: 00:00 to 23:30 lasts 24.5 h, over lesson.MaxDuration.
		{"longer than the limit", []any{"Night", day(2026, 10, 25), clock(0, 0), clock(23, 30), "Europe/Berlin"}, nil, "end", "D"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var edit func(*excelize.File)
			if tt.edit != nil {
				edit = func(f *excelize.File) { tt.edit(t, f) }
			}
			p := parse(t, workbook(t, [][]any{header, ok, tt.row}, edit))
			if len(p.Rows) != 1 || len(p.Errors) != 1 {
				t.Fatalf("parsed = %+v", p)
			}
			e := p.Errors[0]
			prefix := "column " + tt.letter + ": "
			if e.Line != 3 || e.Field != tt.field || !strings.HasPrefix(e.Reason, prefix) || len(e.Reason) == len(prefix) ||
				strings.Contains(e.Reason, secret) {
				t.Fatalf("error = %+v, want line 3 field %q reason %q...", e, tt.field, prefix)
			}
			plan, err := importplan.Build(importplan.SourceXLSX, p, nil)
			if err != nil || plan.Valid() {
				t.Fatalf("a row error must block the plan: %+v, %v", plan, err)
			}
		})
	}
}

func TestColumnLetterFollowsTheHeader(t *testing.T) {
	data := workbook(t, [][]any{
		{"room", "start", "end", "timezone", "date", "notes"},
		{},
	}, func(f *excelize.File) {
		must(t, f.SetCellValue(sheet1, "AB1", "title"))
		must(t, f.SetSheetRow(sheet1, "A2", &[]any{"101", clock(9, 0), clock(10, 0), "UTC", day(2026, 10, 14)}))
		must(t, f.SetCellValue(sheet1, "AB2", " "))
	})
	p := parse(t, data)
	if len(p.Errors) != 1 || p.Errors[0].Field != "title" || !strings.HasPrefix(p.Errors[0].Reason, "column AB: ") || p.Errors[0].Line != 2 {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestHeaderBelowEmptyRowsAndGaps(t *testing.T) {
	data := workbook(t, nil, func(f *excelize.File) {
		must(t, f.SetSheetRow(sheet1, "B3", &header))
		must(t, f.SetSheetRow(sheet1, "B4", &[]any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}))
		must(t, f.SetSheetRow(sheet1, "B9", &[]any{"Bio", day(2026, 10, 14), "bad", clock(10, 0), "UTC"}))
		must(t, f.SetCellValue(sheet1, "H6", "   ")) // a row of blanks is skipped
	})
	p := parse(t, data)
	if len(p.Rows) != 1 || p.Rows[0].Line != 4 || len(p.Errors) != 1 || p.Errors[0].Line != 9 || p.Errors[0].Field != "start" ||
		!strings.HasPrefix(p.Errors[0].Reason, "column D: ") {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestDate1904(t *testing.T) {
	data := workbook(t, nil, func(f *excelize.File) {
		yes := true
		must(t, f.SetWorkbookProps(&excelize.WorkbookPropsOptions{Date1904: &yes}))
		must(t, f.SetSheetRow(sheet1, "A1", &header))
		must(t, f.SetSheetRow(sheet1, "A2", &[]any{"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}))
		must(t, f.SetSheetRow(sheet1, "A3", &[]any{"Epoch", 0, clock(9, 0), clock(10, 0), "UTC"}))
	})
	p := parse(t, data)
	if len(p.Errors) != 0 || len(p.Rows) != 2 ||
		!p.Rows[0].Content.Start.Equal(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)) ||
		!p.Rows[1].Content.Start.Equal(time.Date(1904, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestReadsOnlyTheFirstVisibleSheet(t *testing.T) {
	data := workbook(t, [][]any{header, {"Hidden", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}}, func(f *excelize.File) {
		for _, s := range []string{"Visible", "Other"} {
			_, err := f.NewSheet(s)
			must(t, err)
			must(t, f.SetSheetRow(s, "A1", &header))
			must(t, f.SetSheetRow(s, "A2", &[]any{s, day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}))
		}
		f.SetActiveSheet(1)
		must(t, f.SetSheetVisible(sheet1, false))
	})
	p := parse(t, data)
	if len(p.Rows) != 1 || p.Rows[0].Content.Title != "Visible" {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestFileErrors(t *testing.T) {
	valid := workbook(t, [][]any{header, {"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}}, nil)
	many := [][]any{header}
	for i := 0; i <= importplan.MaxRows; i++ {
		many = append(many, []any{fmt.Sprintf("L%d", i), "2026-10-14", "09:00", "10:00", "UTC"})
	}
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 600)...)

	tests := []struct {
		name string
		data []byte
		kind error
		line int
	}{
		{"empty file", nil, ErrNotWorkbook, 0},
		{"csv text", []byte("title,date,start,end,timezone\n" + secret), ErrNotWorkbook, 0},
		{"encrypted or xls", ole, ErrNotWorkbook, 0},
		{"cut file", valid[:len(valid)/2], ErrNotWorkbook, 0},
		{"too large", append(bytes.Clone(zipMagic), make([]byte, MaxFileBytes)...), ErrTooLarge, 0},
		{"empty sheet", workbook(t, nil, nil), ErrHeader, 0},
		{"missing column", workbook(t, [][]any{nil, {"title", "date", "start", "end", secret}}, nil), ErrHeader, 2},
		{"duplicate column", workbook(t, [][]any{{"title", "date", "start", "end", "timezone", "TITLE"}}, nil), ErrHeader, 1},
		{"too many rows", workbook(t, many, nil), ErrTooMany, 0},
		{"too many parts", withParts(t, valid, MaxParts, nil), ErrTooLarge, 0},
		{"zip bomb", withParts(t, valid, 1, bytes.Repeat([]byte{0}, MaxUnzippedBytes)), ErrTooLarge, 0},
		{"lying size", withLyingPart(t, valid), ErrMalformed, 0},
		{"damaged entry", withDamagedPart(t, valid), ErrMalformed, 0},
		{"cut sheet xml", editPart(t, valid, "xl/worksheets/sheet1.xml", func(b []byte) []byte { return b[:len(b)-30] }), ErrMalformed, 0},
		{"other xml encoding", editPart(t, valid, "xl/worksheets/sheet1.xml", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`encoding="UTF-8"`), []byte(`encoding="windows-1251"`), 1)
		}), ErrMalformed, 0},
		{"no workbook part", editPart(t, valid, "xl/workbook.xml", func([]byte) []byte { return []byte("<x/>") }), ErrMalformed, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(context.Background(), tt.data)
			var e *Error
			if !errors.As(err, &e) || !errors.Is(err, tt.kind) || !errors.Is(err, importplan.ErrUnreadableFile) {
				t.Fatalf("err = %v, want %v wrapping ErrUnreadableFile", err, tt.kind)
			}
			if e.Line != tt.line || strings.Contains(err.Error(), secret) {
				t.Fatalf("err = %v, want line %d without file content", err, tt.line)
			}
		})
	}
}

func TestSafelyRecoversFromPanic(t *testing.T) {
	err := safely(func() error { panic("hostile input") })
	if !errors.Is(err, ErrMalformed) || !errors.Is(err, importplan.ErrUnreadableFile) {
		t.Fatalf("safely = %v", err)
	}
}

func TestParseHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := workbook(t, [][]any{header, {"Math", "2026-10-14", "09:00", "10:00", "UTC"}}, nil)
	if _, err := New().Parse(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestParseIsDeterministic(t *testing.T) {
	data := workbook(t, [][]any{header, {"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "Europe/Moscow"}, {"Bad", "x"}}, nil)
	a, b := parse(t, data), parse(t, data)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("the same bytes gave different results")
	}
}

func TestClockOf(t *testing.T) {
	tests := []struct {
		in   string
		h, m int
		ok   bool
	}{
		{"09:00", 9, 0, true},
		{"9:05", 9, 5, true},
		{"0.375", 9, 0, true},
		{"0.39583334", 9, 30, true}, // float32 precision, as some writers store it
		{"0", 0, 0, true},
		{"0.99930555555555556", 23, 59, true},
		{"1", 0, 0, false},
		{"24:00", 0, 0, false},
		{"0.5000001", 12, 0, true}, // 12:00:00.0086 rounds to 12:00:00
		{"NaN", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tt := range tests {
		h, m, ok := clockOf(tt.in)
		if h != tt.h || m != tt.m || ok != tt.ok {
			t.Errorf("clockOf(%q) = %d:%d %v, want %d:%d %v", tt.in, h, m, ok, tt.h, tt.m, tt.ok)
		}
	}
}

func TestColumnLetters(t *testing.T) {
	for i, want := range map[int]string{0: "A", 2: "C", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA", 16383: "XFD"} {
		if got := columnLetters(i); got != want {
			t.Errorf("columnLetters(%d) = %s, want %s", i, got, want)
		}
	}
}

// rezip copies the zip entries of data through edit (nil drops nothing and changes nothing) and appends extra.
func rezip(t *testing.T, data []byte, edit func(name string, body []byte) []byte, extra func(w *zip.Writer)) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	must(t, err)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, zf := range zr.File {
		rc, err := zf.Open()
		must(t, err)
		body, err := io.ReadAll(rc)
		must(t, err)
		_ = rc.Close()
		if edit != nil {
			body = edit(zf.Name, body)
		}
		fw, err := w.Create(zf.Name)
		must(t, err)
		_, err = fw.Write(body)
		must(t, err)
	}
	if extra != nil {
		extra(w)
	}
	must(t, w.Close())
	return buf.Bytes()
}

func editPart(t *testing.T, data []byte, part string, fn func([]byte) []byte) []byte {
	return rezip(t, data, func(name string, body []byte) []byte {
		if name == part {
			return fn(body)
		}
		return body
	}, nil)
}

// withParts adds n media entries holding body each.
func withParts(t *testing.T, data []byte, n int, body []byte) []byte {
	return rezip(t, data, nil, func(w *zip.Writer) {
		for i := range n {
			fw, err := w.Create(fmt.Sprintf("xl/media/pad%d.bin", i))
			must(t, err)
			_, err = fw.Write(body)
			must(t, err)
		}
	})
}

// withLyingPart adds an entry that unpacks to more bytes than its header declares.
func withLyingPart(t *testing.T, data []byte) []byte {
	return rezip(t, data, nil, func(w *zip.Writer) {
		body := bytes.Repeat([]byte("<a/>"), 1<<16)
		var deflated bytes.Buffer
		fl, err := flate.NewWriter(&deflated, flate.BestCompression)
		must(t, err)
		_, err = fl.Write(body)
		must(t, err)
		must(t, fl.Close())
		fw, err := w.CreateRaw(&zip.FileHeader{
			Name: "xl/media/lie.xml", Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(body),
			CompressedSize64: uint64(deflated.Len()), UncompressedSize64: 100,
		})
		must(t, err)
		_, err = fw.Write(deflated.Bytes())
		must(t, err)
	})
}

// withDamagedPart adds a stored entry whose bytes no longer match its checksum.
func withDamagedPart(t *testing.T, data []byte) []byte {
	const marker = "MARKERMARKERMARKER"
	out := rezip(t, data, nil, func(w *zip.Writer) {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: "xl/media/img.bin", Method: zip.Store})
		must(t, err)
		_, err = fw.Write([]byte(marker))
		must(t, err)
	})
	i := bytes.Index(out, []byte(marker))
	if i < 0 {
		t.Fatal("marker not found")
	}
	out[i] ^= 0xFF
	return out
}

// A row far down the sheet costs only empty steps of the iterator and keeps its row number.
func TestFarRowNumber(t *testing.T) {
	valid := workbook(t, [][]any{header, {"Math", day(2026, 10, 14), clock(9, 0), clock(10, 0), "UTC"}}, nil)
	data := editPart(t, valid, "xl/worksheets/sheet1.xml", func(b []byte) []byte {
		b = bytes.Replace(b, []byte(`<row r="2"`), []byte(`<row r="1048576"`), 1)
		for _, c := range "ABCDE" {
			b = bytes.ReplaceAll(b, []byte(`r="`+string(c)+`2"`), []byte(`r="`+string(c)+`1048576"`))
		}
		return b
	})
	p := parse(t, data)
	if len(p.Errors) != 0 || len(p.Rows) != 1 || p.Rows[0].Line != excelize.TotalRows {
		t.Fatalf("parsed = %+v", p)
	}
}
