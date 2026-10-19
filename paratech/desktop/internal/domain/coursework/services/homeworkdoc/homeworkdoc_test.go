package homeworkdoc

import (
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

func full() Input {
	return Input{
		Student: Student{
			FullName: "Иванов Иван Иванович", Group: "ИВТ-21", University: "МГУ им. М. В. Ломоносова",
			Faculty: "Факультет ВМК", City: "Москва",
		},
		Subject: "Математический анализ", Teacher: "Петров П. П.", AssignmentTitle: "Пределы, задачи 1-5",
		DraftTitle: "Решение задач", DraftBody: "Задача 1.\nОтвет: 2.\n\n## Задача 2\nОтвет: 3.", Year: 2026,
	}
}

func TestTitlePageWithEveryField(t *testing.T) {
	got := Build(full()).Title
	want := values.TitlePage{
		University: "МГУ им. М. В. Ломоносова", Faculty: "Факультет ВМК",
		Heading:   "Домашнее задание по дисциплине «Математический анализ»",
		Topic:     "Пределы, задачи 1-5",
		Performer: "Выполнил: студент группы ИВТ-21 Иванов Иван Иванович",
		Checker:   "Проверил: Петров П. П.",
		Place:     "Москва 2026",
	}
	if got != want {
		t.Fatalf("title page =\n%+v\nwant\n%+v", got, want)
	}
}

// Empty fields are left out with their labels: nothing dangles.
func TestTitlePageSkipsEmptyFields(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Input)
		want   values.TitlePage
	}{
		{"nothing known", func(in *Input) {
			*in = Input{Year: 2026}
		}, values.TitlePage{Heading: "Домашнее задание", Place: "2026"}},
		{"no group", func(in *Input) { in.Student.Group = " " }, values.TitlePage{Performer: "Выполнил: Иванов Иван Иванович"}},
		{"no name", func(in *Input) { in.Student.FullName = "" }, values.TitlePage{Performer: "Выполнил: студент группы ИВТ-21"}},
		{"no student", func(in *Input) { in.Student.FullName, in.Student.Group = "", "" }, values.TitlePage{}},
		{"no teacher", func(in *Input) { in.Teacher = "" }, values.TitlePage{}},
		{"no subject", func(in *Input) { in.Subject = "" }, values.TitlePage{Heading: "Домашнее задание"}},
		{"no city", func(in *Input) { in.Student.City = "" }, values.TitlePage{Place: "2026"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := full()
			tc.change(&in)
			got := Build(in).Title
			switch tc.name {
			case "nothing known":
				if got != tc.want {
					t.Fatalf("title page = %+v", got)
				}
			case "no group", "no name", "no student":
				if got.Performer != tc.want.Performer {
					t.Fatalf("performer = %q, want %q", got.Performer, tc.want.Performer)
				}
			case "no teacher":
				if got.Checker != "" {
					t.Fatalf("checker = %q", got.Checker)
				}
			case "no subject":
				if got.Heading != tc.want.Heading {
					t.Fatalf("heading = %q", got.Heading)
				}
			case "no city":
				if got.Place != tc.want.Place {
					t.Fatalf("place = %q", got.Place)
				}
			}
		})
	}
}

// Profile and lesson texts are cleaned to one line: a line break or a bidi override cannot shape the page.
func TestTitlePageTextsAreOneLine(t *testing.T) {
	in := full()
	in.Student.FullName = "Иванов\nИван\u202e"
	in.Teacher = "Петров\r\nП."
	got := Build(in).Title
	if got.Performer != "Выполнил: студент группы ИВТ-21 Иванов Иван" || got.Checker != "Проверил: Петров П." {
		t.Fatalf("title page = %+v", got)
	}
}

func TestBodyBlocks(t *testing.T) {
	in := full()
	in.DraftTitle = "Решение"
	in.DraftBody = "# Решение\r\n\r\nВведение: строка 1\nстрока 2  \n\n\n## Задача 1\nОтвет: 2.\n### Подпункт\n#### слишком глубоко\n#без пробела\n- пункт\x07 один\n- пункт два\n\n# \n"
	got := Build(in).Blocks
	want := []values.Block{
		{Kind: values.BlockHeading, Level: 1, Lines: []string{"Решение"}},
		{Kind: values.BlockParagraph, Lines: []string{"Введение: строка 1", "строка 2"}},
		{Kind: values.BlockHeading, Level: 2, Lines: []string{"Задача 1"}},
		{Kind: values.BlockParagraph, Lines: []string{"Ответ: 2."}},
		{Kind: values.BlockHeading, Level: 3, Lines: []string{"Подпункт"}},
		{Kind: values.BlockParagraph, Lines: []string{"#### слишком глубоко", "#без пробела", "- пункт один", "- пункт два"}},
		{Kind: values.BlockParagraph, Lines: []string{"#"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks =\n%#v\nwant\n%#v", got, want)
	}
}

func TestEmptyDraft(t *testing.T) {
	got := Build(Input{Year: 2026})
	if len(got.Blocks) != 0 || got.Title.IsZero() {
		t.Fatalf("document = %+v", got)
	}
}
