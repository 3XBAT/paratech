package summaryexport

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/summary"
)

var msk = time.FixedZone("MSK", 3*60*60)

func content(t *testing.T, text string, points ...string) summary.Content {
	t.Helper()
	c, err := summary.NewContent(text, points)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRender(t *testing.T) {
	doc := Document{
		Title:    "География",
		Start:    time.Date(2026, 10, 14, 10, 1, 0, 0, msk),
		Duration: 85 * time.Minute,
		Content:  content(t, "Реки и их дельты.\n\nДельта — устье.", "Реки текут в море", "Дельта"),
		Homework: []Homework{
			{Title: "Прочитать главу 4", Description: "Стр. 40-52", Due: time.Date(2026, 10, 16, 23, 59, 0, 0, msk)},
			{Title: "Нарисовать карту"},
		},
	}
	tests := []struct {
		name string
		doc  Document
		f    Format
		want string
	}{
		{
			name: "text", doc: doc, f: FormatText,
			want: "География\n14.10.2026, 10:01 · 1 ч 25 мин\n\nРеки и их дельты.\n\nДельта — устье.\n" +
				"\nКлючевые моменты:\n- Реки текут в море\n- Дельта\n" +
				"\nДомашние задания:\n- Прочитать главу 4 — до 16.10.2026, 23:59\n  Стр. 40-52\n- Нарисовать карту\n",
		},
		{
			name: "markdown", doc: doc, f: FormatMarkdown,
			want: "# География\n\n14\\.10.2026, 10:01 · 1 ч 25 мин\n\nРеки и их дельты.\n\nДельта — устье.\n" +
				"\n## Ключевые моменты\n\n- Реки текут в море\n- Дельта\n" +
				"\n## Домашние задания\n\n- Прочитать главу 4 — до 16.10.2026, 23:59\n  Стр. 40-52\n- Нарисовать карту\n",
		},
		{
			name: "text only", doc: Document{Content: content(t, "Текст.")}, f: FormatText,
			want: "Текст.\n",
		},
		{
			name: "title without date", doc: Document{Title: "Физика", Content: content(t, "Текст.")}, f: FormatText,
			want: "Физика\n\nТекст.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(tc.doc, tc.f)
			if err != nil || got != tc.want {
				t.Fatalf("Render =\n%q, %v\nwant\n%q", got, err, tc.want)
			}
		})
	}
}

// Model output cannot form markup in Markdown: every line is escaped, indentation that would make a code block is
// dropped, and a multi-line title or assignment stays on its line.
func TestRenderMarkdownEscapesModelText(t *testing.T) {
	doc := Document{
		Title:    "Урок\n# не заголовок",
		Content:  content(t, "<script>alert(1)</script>\n    код\n- пункт\n[ссылка](https://evil.example)", "**жирный**"),
		Homework: []Homework{{Title: "<img src=x>", Description: "строка\n# вторая"}},
	}
	got, err := Render(doc, FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Урок \\# не заголовок\n\n" +
		"\\<script\\>alert(1)\\</script\\>\nкод\n\\- пункт\n\\[ссылка\\](https:\\//evil.example)\n" +
		"\n## Ключевые моменты\n\n- \\*\\*жирный\\*\\*\n" +
		"\n## Домашние задания\n\n- \\<img src=x\\>\n  строка \\# вторая\n"
	if got != want {
		t.Fatalf("Render =\n%q\nwant\n%q", got, want)
	}
	for i, r := range got {
		if strings.ContainsRune("<>[]*", r) && (i == 0 || got[i-1] != '\\') {
			t.Fatalf("unescaped %q at %d", r, i)
		}
	}
}

func TestRenderNormalisesLineBreaks(t *testing.T) {
	got, err := Render(Document{Content: content(t, "a\r\nb")}, FormatText)
	if err != nil || got != "a\nb\n" {
		t.Fatalf("Render = %q, %v", got, err)
	}
}

func TestFormat(t *testing.T) {
	for _, f := range []Format{FormatText, FormatMarkdown} {
		if err := f.Validate(); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	for _, f := range []Format{"", "html", "TXT"} {
		if err := f.Validate(); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%q: err = %v", f, err)
		}
		if _, err := Render(Document{Content: content(t, "x")}, f); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("Render(%q): err = %v", f, err)
		}
	}
	if FormatMarkdown.Extension() != ".md" || FormatText.Extension() != ".txt" {
		t.Fatal("extensions")
	}
}

func TestFileName(t *testing.T) {
	tests := []struct {
		base string
		f    Format
		want string
	}{
		{"Конспект Физика 2026-10-14", FormatMarkdown, "Конспект Физика 2026-10-14.md"},
		{`Конспект a/b:c*d 2026-10-14`, FormatText, "Конспект a_b_c_d 2026-10-14.txt"},
		{"...", FormatText, "summary.txt"},
		{"con", FormatText, "_con.txt"},
	}
	for _, tc := range tests {
		if got := FileName(tc.base, tc.f); got != tc.want {
			t.Errorf("FileName(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-time.Minute, ""},
		{10 * time.Second, "1 мин"},
		{45 * time.Minute, "45 мин"},
		{time.Hour + 29*time.Second, "1 ч"},
		{85*time.Minute + 31*time.Second, "1 ч 26 мин"},
	}
	for _, tc := range tests {
		if got := FormatDuration(tc.d); got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
