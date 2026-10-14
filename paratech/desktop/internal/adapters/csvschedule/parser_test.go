package csvschedule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

func parse(t *testing.T, s string) importplan.Parsed {
	t.Helper()
	p, err := New().Parse(context.Background(), []byte(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestParseValid(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	src := "ï»¿Title,Date,Start,End,TimeZone,UID,Room\r\n" +
		"Math,2026-10-14,09:00,10:30,Europe/Moscow,m-1,101\r\n" +
		"\"Physics, lab\",2026-10-14,9:45,11:15,UTC,,202\r\n" +
		"=SUM(A1),2026-10-15, 12:00 , 13:00 ,Europe/Moscow,,\r\n"
	p := parse(t, src)
	if len(p.Errors) != 0 || len(p.Rows) != 3 {
		t.Fatalf("parsed = %+v", p)
	}

	want := []importplan.Row{
		{Line: 2, Key: "id:m-1", Content: lesson.Content{Title: "Math",
			Start: time.Date(2026, 10, 14, 9, 0, 0, 0, msk), End: time.Date(2026, 10, 14, 10, 30, 0, 0, msk), TimeZone: "Europe/Moscow"}},
		{Line: 3, Key: "auto:2026-10-14 09:45 Physics, lab", Content: lesson.Content{Title: "Physics, lab",
			Start: time.Date(2026, 10, 14, 9, 45, 0, 0, time.UTC), End: time.Date(2026, 10, 14, 11, 15, 0, 0, time.UTC), TimeZone: "UTC"}},
		{Line: 4, Key: "auto:2026-10-15 12:00 =SUM(A1)", Content: lesson.Content{Title: "=SUM(A1)",
			Start: time.Date(2026, 10, 15, 12, 0, 0, 0, msk), End: time.Date(2026, 10, 15, 13, 0, 0, 0, msk), TimeZone: "Europe/Moscow"}},
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
	if !p.Rows[0].Content.Start.Equal(time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("Moscow 09:00 is %v, want 06:00 UTC", p.Rows[0].Content.Start.UTC())
	}
}

func TestParseSemicolonAndMultiline(t *testing.T) {
	p := parse(t, "title;date;start;end;timezone\n\"Math\nseminar\";2026-10-14;09:00;10:00;UTC\nBio;2026-10-14;bad;10:00;UTC\n")
	if len(p.Rows) != 1 || p.Rows[0].Line != 2 {
		t.Fatalf("rows = %+v", p.Rows)
	}
	// The quoted title spans lines 2 and 3, so the next record starts on line 4.
	if len(p.Errors) != 1 || p.Errors[0].Line != 4 || p.Errors[0].Field != colStart {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestParseRowErrors(t *testing.T) {
	const header = "title,date,start,end,timezone\n"
	tests := []struct {
		name  string
		row   string
		field string
	}{
		{"empty title", " ,2026-10-14,09:00,10:00,UTC", colTitle},
		{"bad date", "Math,14.10.2026,09:00,10:00,UTC", colDate},
		{"impossible date", "Math,2026-02-30,09:00,10:00,UTC", colDate},
		{"bad start", "Math,2026-10-14,9am,10:00,UTC", colStart},
		{"hour 24", "Math,2026-10-14,24:00,10:00,UTC", colStart},
		{"bad end", "Math,2026-10-14,09:00,,UTC", colEnd},
		{"end before start", "Math,2026-10-14,10:00,09:00,UTC", colEnd},
		{"end equals start", "Math,2026-10-14,10:00,10:00,UTC", colEnd},
		{"unknown zone", "Math,2026-10-14,09:00,10:00,Mars/Base", colTimeZone},
		{"empty zone", "Math,2026-10-14,09:00,10:00,", colTimeZone},
		{"local zone", "Math,2026-10-14,09:00,10:00,Local", colTimeZone},
		{"path zone", "Math,2026-10-14,09:00,10:00,../../etc/passwd", colTimeZone},
		{"dst gap", "Math,2026-03-29,02:30,03:30,Europe/Berlin", colStart},
		{"too few fields", "Math,2026-10-14,09:00", ""},
		{"too many fields", "Math,2026-10-14,09:00,10:00,UTC,x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parse(t, header+"Ok,2026-10-14,08:00,08:30,UTC\n"+tt.row+"\n")
			if len(p.Rows) != 1 || len(p.Errors) != 1 {
				t.Fatalf("parsed = %+v", p)
			}
			if e := p.Errors[0]; e.Line != 3 || e.Field != tt.field || e.Reason == "" {
				t.Fatalf("error = %+v, want line 3 field %q", e, tt.field)
			}
		})
	}
}

func TestParseFileErrors(t *testing.T) {
	many := strings.Builder{}
	many.WriteString("title,date,start,end,timezone\n")
	for i := 0; i <= importplan.MaxRows; i++ {
		fmt.Fprintf(&many, "L%d,2026-10-14,09:00,10:00,UTC\n", i)
	}
	tests := []struct {
		name string
		data string
		kind error
		line int
	}{
		{"empty", "", ErrHeader, 0},
		{"bom only", "ï»¿", ErrHeader, 0},
		{"missing column", "title,date,start,end\nMath,2026-10-14,09:00,10:00\n", ErrHeader, 1},
		{"duplicate column", "title,date,start,end,timezone,Title\n", ErrHeader, 1},
		{"not utf-8", "title,date,start,end,timezone\nMa\xffth,2026-10-14,09:00,10:00,UTC\n", ErrEncoding, 0},
		{"bare quote", "title,date,start,end,timezone\nOk,2026-10-14,08:00,09:00,UTC\nMa\"th,2026-10-14,09:00,10:00,UTC\n", ErrMalformed, 3},
		{"too large", strings.Repeat("x", MaxFileBytes+1), ErrTooLarge, 0},
		{"too many rows", many.String(), ErrTooMany, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(context.Background(), []byte(tt.data))
			var e *Error
			if !errors.As(err, &e) || !errors.Is(err, tt.kind) || !errors.Is(err, importplan.ErrUnreadableFile) {
				t.Fatalf("err = %v, want %v wrapping ErrUnreadableFile", err, tt.kind)
			}
			if e.Line != tt.line {
				t.Errorf("line = %d, want %d", e.Line, tt.line)
			}
		})
	}
}

func TestParseHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Parse(ctx, []byte("title,date,start,end,timezone\nMath,2026-10-14,09:00,10:00,UTC\n")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestParseIsDeterministic(t *testing.T) {
	src := "title,date,start,end,timezone\nMath,2026-10-14,09:00,10:00,Europe/Moscow\nBad,x,09:00,10:00,UTC\n"
	a, b := parse(t, src), parse(t, src)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("the same bytes gave different results")
	}
}
