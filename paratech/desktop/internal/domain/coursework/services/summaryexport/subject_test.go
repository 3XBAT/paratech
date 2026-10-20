package summaryexport

import (
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/summary"
)

func TestRenderSubject(t *testing.T) {
	content, err := summary.NewContent("Сила — это <b>вектор</b>.", []string{"Законы Ньютона", "# не заголовок"})
	if err != nil {
		t.Fatal(err)
	}
	doc := SubjectDocument{
		Subject: "Физика *1*",
		Lessons: []SubjectLesson{
			{
				Title: "Механика", Kind: "lecture", Start: time.Date(2026, 10, 14, 10, 1, 0, 0, msk), Duration: 90 * time.Minute,
				Summary: content,
				Blocks:  []lecturetext.Block{lectureBlock(t, "Введение", 65_000, "Начнём.", "[ссылка](http://x.example)")},
			},
			{Kind: "unknown", Start: time.Date(2026, 10, 19, 12, 0, 0, 0, msk)},
		},
	}
	want := "# Физика \\*1\\*\n" +
		"\n## 14\\.10.2026, 10:01 · лекция · Механика\n\n1 ч 30 мин\n\n" +
		"### Кратко\n\nСила — это \\<b\\>вектор\\</b\\>.\n\n#### Ключевые моменты\n\n- Законы Ньютона\n- \\# не заголовок\n" +
		"\n### Текст лекции\n\n#### \\[01:05\\] Введение\n\nНачнём.\n\n\\[ссылка\\](http:\\//x.example)\n" +
		"\n## 19\\.10.2026, 12:00 · Запись\n\n" +
		"### Кратко\n\nКонспекта пока нет.\n\n### Текст лекции\n\nТекста лекции пока нет.\n"
	if got := RenderSubject(doc); got != want {
		t.Fatalf("got\n%s\nwant\n%s\n%q", got, want, got)
	}

	// No line of model text can start a heading or close the subject's structure.
	evil := SubjectDocument{Subject: "S", Lessons: []SubjectLesson{{Title: "a\n# b", Blocks: []lecturetext.Block{lectureBlock(t, "h", 0, "x\n## y")}}}}
	for _, l := range strings.Split(RenderSubject(evil), "\n") {
		if strings.HasPrefix(l, "# ") && l != "# S" || strings.HasPrefix(l, "## y") {
			t.Fatalf("model text made a heading: %q", l)
		}
	}
}

func TestSubjectFileName(t *testing.T) {
	tests := []struct{ subject, want string }{
		{"Физика", "Физика.md"},
		{`Мат: анализ/2`, "Мат_ анализ_2.md"},
		{"", "База знаний.md"},
		{"...", "База знаний.md"},
	}
	for _, tc := range tests {
		if got := SubjectFileName(tc.subject); got != tc.want {
			t.Errorf("SubjectFileName(%q) = %q, want %q", tc.subject, got, tc.want)
		}
	}
}

func TestKindText(t *testing.T) {
	tests := map[string]string{
		"lecture": "лекция", "seminar": "семинар", "practice": "практика", "lab": "лабораторная",
		"": "", "unknown": "", "Lecture": "",
	}
	for kind, want := range tests {
		if got := kindText(kind); got != want {
			t.Errorf("kindText(%q) = %q, want %q", kind, got, want)
		}
	}
}
