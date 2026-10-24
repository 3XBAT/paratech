package homeworkdoc

import (
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

func talkInput() TalkInput {
	return TalkInput{
		Student: Student{FullName: "Иванов Иван", Group: "ИВТ-21", City: "Москва"},
		Subject: "История", Teacher: "Петров П. П.", Topic: "Реформы Петра I", Year: 2026,
		Template: values.PresentationGraphite,
		Slides: []values.Slide{
			{Kind: values.SlideTitle, Title: "Реформы Петра I", Notes: "Здравствуйте."},
			{Kind: values.SlideBullets, Title: "Армия" + string(rune(0x202E)), Bullets: []string{"Рекруты"}, Notes: "Первый абзац.\nВторой абзац."},
			{Kind: values.SlideConclusion, Title: "Выводы", Notes: "Спасибо."},
		},
		Questions: []TalkQuestion{{Question: "Зачем?", Answer: "Чтобы" + string(rune(0x2066)) + " усилить армию."}},
	}
}

func TestBuildPresentation(t *testing.T) {
	in := talkInput()
	p := BuildPresentation(in)
	if err := p.Validate(); err != nil || p.Template != values.PresentationGraphite || len(p.Slides) != 3 {
		t.Fatalf("presentation = %+v, %v", p, err)
	}
	want := "Выполнил: студент группы ИВТ-21 Иванов Иван\nДисциплина: История\nПроверил: Петров П. П.\nМосква 2026"
	if p.Slides[0].Subtitle != want || p.Slides[1].Subtitle != "" {
		t.Fatalf("subtitle = %q", p.Slides[0].Subtitle)
	}
	if in.Slides[0].Subtitle != "" {
		t.Fatal("the input was changed")
	}

	// Long names never break the limits of the title slide: lines that do not fit are left out.
	in.Subject, in.Teacher = strings.Repeat("я", 190), strings.Repeat("ю", 190)
	sub := BuildPresentation(in).Slides[0].Subtitle
	if utf8.RuneCountInString(sub) > values.MaxSlideSubtitleRunes || strings.Count(sub, "\n") >= values.MaxSlideSubtitleLines {
		t.Fatalf("subtitle of %d runes", utf8.RuneCountInString(sub))
	}
	if err := BuildPresentation(in).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTalk(t *testing.T) {
	doc := BuildTalk(talkInput())
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if doc.Title.Heading != "Текст выступления к презентации по дисциплине «История»" || doc.Title.Topic != "на тему: «Реформы Петра I»" ||
		doc.Title.Checker != "Проверил: Петров П. П." || doc.Blocks[0].Lines[0] != GeneratedMark {
		t.Fatalf("title = %+v", doc.Title)
	}
	var lines []string
	for _, b := range doc.Blocks {
		lines = append(lines, strings.Join(b.Lines, " "))
	}
	got := strings.Join(lines, "|")
	want := GeneratedMark + "|Слайд 1. Реформы Петра I|Здравствуйте.|Слайд 2. Армия|Первый абзац.|Второй абзац.|" +
		"Слайд 3. Выводы|Спасибо.|" + QuestionsHeading + "|1. Зачем?|Ответ: Чтобы усилить армию."
	if got != want {
		t.Fatalf("talk =\n%s\nwant\n%s", got, want)
	}
}

func TestPresentationFileNames(t *testing.T) {
	if got := PresentationFileName("Реформы: Петра I?"); !strings.HasSuffix(got, PptxExtension) || strings.ContainsAny(got, ":?") ||
		!strings.HasPrefix(got, "Презентация ") {
		t.Fatalf("pptx name = %q", got)
	}
	if got := TalkFileName("Реформы"); got != "Текст выступления Реформы"+DocxExtension {
		t.Fatalf("talk name = %q", got)
	}
	if got := PresentationFileName(""); got != "Презентация"+PptxExtension {
		t.Fatalf("empty topic = %q", got)
	}
}
