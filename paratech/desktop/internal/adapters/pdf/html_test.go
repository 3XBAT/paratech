package pdf

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/values"
)

func student() homeworkdoc.Student {
	return homeworkdoc.Student{
		FullName: "Иванов Иван Иванович", Group: "Ф-21", University: "Тестовый университет",
		Faculty: "Физический факультет", City: "Москва",
	}
}

func paperInput() homeworkdoc.PaperInput {
	return homeworkdoc.PaperInput{
		Student: student(), Subject: "Физика", Teacher: "Петров П. П.", Kind: "реферат", Topic: "Колебания маятника",
		Year: 2026,
		Sections: []homeworkdoc.PaperSection{
			{Title: "Введение", Paragraphs: []string{"Маятник — тело, совершающее колебания."}},
			{Title: "Теория", Paragraphs: []string{"Период зависит от длины."}},
		},
		Sources: []homeworkdoc.PaperSource{{Text: "Сивухин Д. В. Общий курс физики."}, {Text: "Модель назвала", Verify: true}},
	}
}

// every is a document with every block kind, as the docx tests use.
func every() values.Document {
	return values.Document{
		Title:             values.TitlePage{Heading: "Реферат", Place: "Москва 2026"},
		TableOfContents:   true,
		SectionsOnNewPage: true,
		Blocks: []values.Block{
			{Kind: values.BlockHeading, Level: 1, Lines: []string{"Введение"}},
			{Kind: values.BlockParagraph, Lines: []string{"Первая строка", "вторая\tс табуляцией"}},
			{Kind: values.BlockBulletList, Lines: []string{"математический", "физический"}},
			{Kind: values.BlockHeading, Level: 2, Lines: []string{"Период"}},
			{Kind: values.BlockHeading, Level: 9, Lines: []string{"Погрешности"}},
			{Kind: values.BlockNumberedList, Lines: []string{"измерить", "засечь"}},
			{Kind: values.BlockTable, Caption: "Результаты", Table: values.Table{
				Header: []string{"Длина, м", "Период, с"},
				Rows:   [][]string{{"0,5", "1,42"}, {"1,5"}},
			}},
			{Kind: values.BlockTable, Table: values.Table{Rows: [][]string{{"g", "9,81"}}}},
			{Kind: values.BlockPlaceholder, Lines: []string{"Вставьте график"}, Caption: "Зависимость T(l)"},
			{Kind: values.BlockPlaceholder, Lines: []string{"Вставьте измерения"}},
			{Kind: values.BlockCaption, Lines: []string{"Рисунок 2 – Схема"}},
			{Kind: values.BlockBibliography, Lines: []string{"Первый источник", "Второй источник"}},
		},
	}
}

// colons is a fixed wording as the page holds it: the page escapes ':'.
func colons(s string) string { return strings.ReplaceAll(s, ":", "&#58;") }

func render(t *testing.T, doc values.Document) string {
	t.Helper()
	out, err := printHTML(doc)
	if err != nil {
		t.Fatalf("printHTML: %v", err)
	}
	return string(out)
}

// inOrder fails unless every fragment occurs in s, each after the previous one.
func inOrder(t *testing.T, s string, fragments []string) {
	t.Helper()
	rest := s
	for _, f := range fragments {
		i := strings.Index(rest, f)
		if i < 0 {
			t.Fatalf("missing (or out of order) %q in\n%s", f, s)
		}
		rest = rest[i+len(f):]
	}
}

// elements returns the element names of the page in order; it fails on a page that is not well-formed.
func elements(t *testing.T, s string) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(s))
	var out []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("not well-formed: %v\n%s", err, s)
		}
		if el, ok := tok.(xml.StartElement); ok {
			out = append(out, el.Name.Local)
		}
	}
}

func TestPrintHTMLStructure(t *testing.T) {
	head := []string{
		"<!DOCTYPE html>", `<html lang="ru">`, `<meta charset="utf-8"/>`,
		`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"/>`,
		`@page{size:A4;margin:20mm 15mm 20mm 30mm;`, `@page:first{@bottom-center{content:none}}`,
		`body{margin:0;font-family:"Times New Roman";font-size:14pt;line-height:1.5}`, `text-indent:12.5mm;text-align:justify`,
		`h1,h2,h3{font-weight:bold;text-align:left;margin:12pt 0 6pt;break-after:avoid}h3{font-style:italic}`,
		`.big{font-size:16pt}.g1{margin-top:180pt}.g2{margin-top:6pt}.g3{margin-top:120pt}.g4{margin-top:120pt}`,
		`.toc1{text-indent:0;text-align:left;margin-left:0pt}.toc2{text-indent:0;text-align:left;margin-left:14.15pt}`,
		`.hl{background-color:#ffff00}`, `.tab{display:inline-block;width:35.45pt}`,
		`table{width:100%;table-layout:fixed;border-collapse:collapse}`,
		`td,th{border:0.5pt solid #000;padding:0 5.4pt;vertical-align:top;text-align:left;line-height:1;font-weight:normal}`,
		`</style></head><body>`,
	}
	title := []string{
		`<p class="ni c">Тестовый университет</p>`, `<p class="ni c">Физический факультет</p>`,
		`<p class="ni c b big g1">Реферат по дисциплине «Физика»</p>`, `<p class="ni c g2">на тему&#58; «Колебания маятника»</p>`,
		`<p class="ni r g3">Выполнил&#58; студент группы Ф-21 Иванов Иван Иванович</p>`, `<p class="ni r">Проверил&#58; Петров П. П.</p>`,
		`<p class="ni c g4">Москва 2026</p>`, pageBreak,
		`<p class="ni c b">СОДЕРЖАНИЕ</p>`,
	}
	lab := homeworkdoc.LabInput{
		Paper: paperInput(),
		Sections: []homeworkdoc.LabSection{
			{Title: "Ход работы", Paragraphs: []homeworkdoc.LabParagraph{
				{Text: "Собрали установку."}, {Text: "Вставьте таблицу измерений", Placeholder: true},
			}},
			{Title: "Выводы", Paragraphs: []homeworkdoc.LabParagraph{{Text: "Период растёт с длиной."}}, Conclusions: true},
		},
		Manual: homeworkdoc.ManualNotRead,
	}
	tests := []struct {
		name string
		doc  values.Document
		want []string
	}{
		{
			name: "paper",
			doc:  homeworkdoc.BuildPaper(paperInput()),
			want: slices.Concat(head, title, []string{
				`<p class="toc1">Введение</p>`, `<p class="toc1">Теория</p>`, `<p class="toc1">` + values.BibliographyHeading + `</p>`,
				pageBreak, `<p>` + homeworkdoc.GeneratedMark + `</p>`,
				`<h1 class="np">Введение</h1>`, `<p>Маятник — тело, совершающее колебания.</p>`,
				`<h1 class="np">Теория</h1>`, `<p>Период зависит от длины.</p>`,
				`<h1 class="ni c np">` + values.BibliographyHeading + `</h1>`,
				`<p>1. Сивухин Д. В. Общий курс физики.</p>`, `<p>2. Модель назвала` + homeworkdoc.VerifySource + `</p>`,
				`</body></html>`,
			}),
		},
		{
			name: "lab report",
			doc:  homeworkdoc.BuildLabReport(lab),
			want: slices.Concat(head, title, []string{
				`<p class="toc1">Ход работы</p>`, `<p class="toc1">Выводы</p>`, `<p class="toc1">` + homeworkdoc.TodoHeading + `</p>`,
				pageBreak, `<p>` + homeworkdoc.GeneratedMark + `</p>`,
				`<p class="ni c"><span class="hl">▢ ` + colons(homeworkdoc.ManualNotRead) + `</span></p>`,
				`<h1 class="np">Ход работы</h1>`, `<p>Собрали установку.</p>`,
				`<p class="ni c"><span class="hl">▢ Вставьте таблицу измерений</span></p>`,
				`<h1 class="np">Выводы</h1>`, `<p>Период растёт с длиной.</p>`,
				`<p class="ni c"><span class="hl">▢ ` + colons(homeworkdoc.ConclusionsNote) + `</span></p>`,
				`<h1 class="ni c np">` + values.BibliographyHeading + `</h1>`,
				`<h1 class="np">` + homeworkdoc.TodoHeading + `</h1>`,
				`<p>1) «Ход работы»&#58; Вставьте таблицу измерений</p>`, `<p>2) «Выводы»&#58; проверьте выводы по своим данным</p>`,
				`<p>3) ` + colons(homeworkdoc.ManualNotRead) + `</p>`, `</body></html>`,
			}),
		},
		{
			name: "every block kind",
			doc:  every(),
			want: []string{
				`<p class="ni c b big g1">Реферат</p>`, `<p class="ni c g4">Москва 2026</p>`, pageBreak,
				`<p class="ni c b">СОДЕРЖАНИЕ</p>`, `<p class="toc1">Введение</p>`, `<p class="toc2">Период</p>`,
				`<p class="toc3">Погрешности</p>`, `<p class="toc1">` + values.BibliographyHeading + `</p>`, pageBreak,
				`<h1>Введение</h1>`, // the first heading of a page needs no break
				`<p>Первая строка<br/>вторая` + tab + `с табуляцией</p>`,
				`<p>– математический</p>`, `<p>– физический</p>`,
				`<h2>Период</h2>`, `<h3>Погрешности</h3>`, `<p>1) измерить</p>`, `<p>2) засечь</p>`,
				`<p class="ni l keep">Таблица 1 – Результаты</p>`,
				`<table><thead><tr><th>Длина, м</th><th>Период, с</th></tr></thead>`,
				`<tbody><tr><td>0,5</td><td>1,42</td></tr><tr><td>1,5</td><td><br/></td></tr></tbody></table>`,
				`<p class="ni l keep">Таблица 2</p>`, `<table><tbody><tr><td>g</td><td>9,81</td></tr></tbody></table>`,
				`<p class="ni c keep"><span class="hl">▢ Вставьте график</span></p>`, `<p class="ni c">Рисунок 1 – Зависимость T(l)</p>`,
				`<p class="ni c"><span class="hl">▢ Вставьте измерения</span></p>`,
				`<p class="ni c">Рисунок 2 – Схема</p>`,
				`<h1 class="ni c np">` + values.BibliographyHeading + `</h1>`, `<p>1. Первый источник</p>`, `<p>2. Второй источник</p>`,
			},
		},
		{
			name: "homework draft: no title, no TOC, no new pages",
			doc: values.Document{Blocks: []values.Block{
				{Kind: values.BlockHeading, Level: 1, Lines: []string{"Задача 1"}},
				{Kind: values.BlockParagraph, Lines: []string{"Решение"}},
				{Kind: values.BlockHeading, Level: 1, Lines: []string{"Задача 2"}},
				{Kind: values.BlockParagraph},
				{Kind: values.BlockParagraph, Lines: []string{"строка", ""}},
			}},
			want: []string{
				`</style></head><body><h1>Задача 1</h1><p>Решение</p><h1>Задача 2</h1><p><br/></p><p>строка<br/><br/></p></body></html>`,
			},
		},
		{
			name: "formatting profile",
			doc: values.Document{
				Format: values.Formatting{FontName: "Arial", FontSizePt: 12, LineSpacingPct: 100, MarginTopMM: 25,
					MarginRightMM: 10, MarginBottomMM: 15, MarginLeftMM: 20, IndentTenthMM: 100},
				Blocks: []values.Block{{Kind: values.BlockParagraph, Lines: []string{"текст"}}},
			},
			want: []string{
				`@page{size:A4;margin:25mm 10mm 15mm 20mm;@bottom-center{content:counter(page);font-family:"Arial";font-size:12pt}}`,
				`body{margin:0;font-family:"Arial";font-size:12pt;line-height:1}`, `text-indent:10mm;`,
			},
		},
		{
			name: "invalid profile falls back to GOST",
			doc: values.Document{
				Format: values.Formatting{FontName: `x"}body{color:red`, FontSizePt: 99, MarginTopMM: 1},
				Blocks: []values.Block{{Kind: values.BlockParagraph, Lines: []string{"текст"}}},
			},
			want: []string{`@page{size:A4;margin:20mm 15mm 20mm 30mm;`, `font-family:"Times New Roman";font-size:14pt`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := render(t, tc.doc)
			inOrder(t, s, tc.want)
			elements(t, s)
			if !tc.doc.Title.IsZero() != strings.Contains(s, `@page:first`) {
				t.Errorf("the first page footer is hidden only with a title page")
			}
		})
	}
}

func TestPrintHTMLInjectionStaysText(t *testing.T) {
	attacks := []string{
		`<script>alert(1)</script>`,
		`</style><script>x()</script>`,
		`"><img src=x onerror=alert(1)>`,
		`<a href="https://example.invalid/">ссылка</a>`,
		"<link rel=stylesheet href=http://example.invalid/x.css>",
		"\u202Eтекст наоборот\u202C и \u2066изоляция\u2069 \u200F",
		"нулевой\x00символ и \x1b[31m и \u0085",
	}
	allowed := []string{"html", "head", "meta", "title", "style", "body", "p", "h1", "h2", "h3", "br", "div", "span",
		"table", "thead", "tbody", "tr", "th", "td"}
	for _, a := range attacks {
		doc := values.Document{
			Title:           values.TitlePage{University: a, Heading: a, Topic: a, Performer: a, Checker: a, Place: a},
			TableOfContents: true,
			Blocks: []values.Block{
				{Kind: values.BlockHeading, Level: 1, Lines: []string{a}},
				{Kind: values.BlockParagraph, Lines: []string{a, a}},
				{Kind: values.BlockBulletList, Lines: []string{a}},
				{Kind: values.BlockNumberedList, Lines: []string{a}},
				{Kind: values.BlockTable, Caption: a, Table: values.Table{Header: []string{a}, Rows: [][]string{{a, a}}}},
				{Kind: values.BlockCaption, Lines: []string{a}},
				{Kind: values.BlockPlaceholder, Lines: []string{a}, Caption: a},
				{Kind: values.BlockBibliography, Lines: []string{a}},
			},
		}
		t.Run(a, func(t *testing.T) {
			s := render(t, doc)
			low := strings.ToLower(s)
			for _, bad := range []string{"<script", "<img", "<a ", "<link", "http:", "https:", "src=", "href=", "url(", "@import"} {
				if strings.Contains(low, bad) {
					t.Errorf("output contains %q", bad)
				}
			}
			if n := strings.Count(s, "</style>"); n != 1 {
				t.Errorf("</style> occurs %d times, want 1", n)
			}
			for _, r := range "\u202E\u202C\u2066\u2069\u200F\x00\x1b\u0085" {
				if strings.ContainsRune(s, r) {
					t.Errorf("output contains %U", r)
				}
			}
			for _, el := range elements(t, s) {
				if !slices.Contains(allowed, el) {
					t.Errorf("unexpected element <%s>", el)
				}
			}
			if strings.Contains(a, "<script>") && !strings.Contains(s, "&lt;script&gt;") {
				t.Errorf("the attack is not kept as escaped text")
			}
		})
	}
}

func TestPrintHTMLNoNetworkNoScript(t *testing.T) {
	docs := map[string]values.Document{
		"paper": homeworkdoc.BuildPaper(paperInput()),
		"every": every(),
		"empty": {},
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			s := strings.ToLower(render(t, doc))
			for _, bad := range []string{"http:", "https:", "src=", "<script", "href=", "url(", "@import"} {
				if strings.Contains(s, bad) {
					t.Errorf("output contains %q", bad)
				}
			}
		})
	}
}

func TestPrintHTMLDeterministic(t *testing.T) {
	for _, doc := range []values.Document{homeworkdoc.BuildPaper(paperInput()), every()} {
		a, err := printHTML(doc)
		if err != nil {
			t.Fatal(err)
		}
		b, err := printHTML(doc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Error("the same document gave different bytes")
		}
	}
}

func TestPrintHTMLRefusesInvalidDocument(t *testing.T) {
	tests := []struct {
		name string
		doc  values.Document
	}{
		{"unknown block kind", values.Document{Blocks: []values.Block{{Kind: "formula", Lines: []string{"x"}}}}},
		{"empty table", values.Document{Blocks: []values.Block{{Kind: values.BlockTable}}}},
		{"too many blocks", values.Document{Blocks: make([]values.Block, values.MaxDocumentBlocks+1)}},
		{"text too long", values.Document{Blocks: []values.Block{{Kind: values.BlockParagraph,
			Lines: []string{strings.Repeat("я", values.MaxTextRunes+1)}}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := printHTML(tc.doc)
			if !errors.Is(err, ErrRender) || !errors.Is(err, values.ErrInvalidDocument) || out != nil {
				t.Fatalf("printHTML = %d bytes, %v; want ErrRender and ErrInvalidDocument", len(out), err)
			}
		})
	}
}

// block must refuse a kind it does not know even if Validate let it through.
func TestBlockUnknownKind(t *testing.T) {
	p := &page{}
	if err := p.block(values.Block{Kind: "slide"}); err == nil {
		t.Fatal("an unknown kind was skipped")
	}
}
