package transcript

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatOffset(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{-5, "00:00"},
		{0, "00:00"},
		{999, "00:00"},
		{61_500, "01:01"},
		{3_599_999, "59:59"},
		{3_600_000, "01:00:00"},
		{3*3_600_000 + 25*60_000 + 7_000, "03:25:07"},
		{MaxOffsetMs, "24:00:00"},
	}
	for _, tc := range tests {
		if got := FormatOffset(tc.ms); got != tc.want {
			t.Errorf("FormatOffset(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestExportFormat(t *testing.T) {
	for _, f := range []ExportFormat{FormatText, FormatMarkdown} {
		if err := f.Validate(); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	for _, f := range []ExportFormat{"", "html", "TXT", "txt "} {
		if err := f.Validate(); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%q: error = %v, want ErrUnknownFormat", f, err)
		}
	}
	if FormatMarkdown.Extension() != ".md" || FormatText.Extension() != ".txt" {
		t.Fatal("extensions")
	}
}

func TestExportFileName(t *testing.T) {
	tests := []struct {
		base string
		f    ExportFormat
		want string
	}{
		{"Математика 2026-10-19", FormatText, "Математика 2026-10-19.txt"},
		{`Физика: "волны" 2026-10-19`, FormatMarkdown, "Физика_ _волны_ 2026-10-19.md"},
		{"CON", FormatText, "_CON.txt"},
		{"...", FormatText, "transcript.txt"},
		{"", FormatMarkdown, "transcript.md"},
	}
	for _, tc := range tests {
		if got := ExportFileName(tc.base, tc.f); got != tc.want {
			t.Errorf("ExportFileName(%q, %s) = %q, want %q", tc.base, tc.f, got, tc.want)
		}
	}
}

var exportSegments = []Segment{
	{0, 2000, "Добрый день."},
	{2000, 3000, "  "},
	{65_000, 70_000, "Тема: *волны* <b>и</b> [ссылка](http://x.ru)"},
	{3_700_000, 3_701_000, "Конец"},
}

func TestRenderText(t *testing.T) {
	got, err := Render("Физика", exportSegments, FormatText, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "Физика\n\n[00:00] Добрый день.\n[01:05] Тема: *волны* <b>и</b> [ссылка](http://x.ru)\n[01:01:40] Конец\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}

	got, err = Render("", exportSegments, FormatText, false)
	if err != nil {
		t.Fatal(err)
	}
	want = "Добрый день.\nТема: *волны* <b>и</b> [ссылка](http://x.ru)\nКонец\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestRenderMarkdown(t *testing.T) {
	got, err := Render("Физика\n# 2", exportSegments, FormatMarkdown, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Физика \\# 2\n\n" +
		"**00:00** Добрый день.\n\n" +
		"**01:05** Тема: \\*волны\\* \\<b\\>и\\</b\\> \\[ссылка\\](http:\\//x.ru)\n\n" +
		"**01:01:40** Конец\n\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}

	got, err = Render("", []Segment{{0, 1, "Hi"}}, FormatMarkdown, false)
	if err != nil || got != "Hi\n\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct{ in, want string }{
		{"обычный текст, без разметки", "обычный текст, без разметки"},
		{`a\b`, `a\\b`},
		{"`code`", "\\`code\\`"},
		{"_x_ ~~y~~ | z", "\\_x\\_ \\~\\~y\\~\\~ \\| z"},
		{"![img](u)", "\\!\\[img\\](u)"},
		{"<script>alert(1)</script>", "\\<script\\>alert(1)\\</script\\>"},
		{"&lt;b&gt;", "&amp;lt;b&amp;gt;"},
		{"# заголовок", "\\# заголовок"},
		{"- пункт", "\\- пункт"},
		{"+ пункт", "\\+ пункт"},
		{"=== ", "\\=== "},
		{"a - b # c", "a - b \\# c"},
		{"Физика #", "Физика \\#"},
		{"1. первое", "1\\. первое"},
		{"12) второе", "12\\) второе"},
		{"2024 год", "2024 год"},
		{"https://example.com", "https:\\//example.com"},
		{"зайдите на www.example.com", "зайдите на www\\.example.com"},
		{"WWW.EXAMPLE.COM", "WWW\\.EXAMPLE.COM"},
		{"почта a@b.ru", "почта a\\@b.ru"},
	}
	for _, tc := range tests {
		if got := escapeMarkdown(tc.in); got != tc.want {
			t.Errorf("escapeMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderLimits(t *testing.T) {
	if _, err := Render("", nil, "html", false); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("unknown format: %v", err)
	}
	big := strings.Repeat("*", MaxSegmentTextBytes)
	n := MaxExportBytes/(2*MaxSegmentTextBytes) + 2
	segs := make([]Segment, n)
	for i := range segs {
		segs[i] = Segment{0, 0, big}
	}
	if _, err := Render("", segs, FormatMarkdown, false); !errors.Is(err, ErrExportTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}
