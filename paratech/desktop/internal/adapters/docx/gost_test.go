package docx

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

// paper is a document with every block kind, Russian text, a TOC and a bibliography.
func paper() values.Document {
	return values.Document{
		Title: values.TitlePage{
			University: "Московский государственный университет", Faculty: "Физический факультет",
			Heading: "Реферат по дисциплине «Физика»", Topic: "Колебания маятника",
			Performer: "Выполнил: студент группы Ф-21 Иванов И. И.", Checker: "Проверил: Петров П. П.", Place: "Москва 2026",
		},
		TableOfContents:   true,
		SectionsOnNewPage: true,
		Blocks: []values.Block{
			{Kind: values.BlockHeading, Level: 1, Lines: []string{"Введение"}},
			{Kind: values.BlockParagraph, Lines: []string{"Маятник — тело, совершающее колебания."}},
			{Kind: values.BlockBulletList, Lines: []string{"математический маятник", "физический маятник"}},
			{Kind: values.BlockHeading, Level: 1, Lines: []string{"Теория"}},
			{Kind: values.BlockHeading, Level: 2, Lines: []string{"Период колебаний"}},
			{Kind: values.BlockNumberedList, Lines: []string{"измерить длину", "засечь время"}},
			{Kind: values.BlockTable, Caption: "Результаты измерений", Table: values.Table{
				Header: []string{"Длина, м", "Период, с"},
				Rows:   [][]string{{"0,5", "1,42"}, {"1,0", "2,01"}, {"1,5"}},
			}},
			{Kind: values.BlockHeading, Level: 3, Lines: []string{"Погрешности"}},
			{Kind: values.BlockNumberedList, Lines: []string{"систематические", "случайные"}},
			{Kind: values.BlockPlaceholder, Lines: []string{"Вставьте график зависимости периода от длины"}, Caption: "Зависимость T(l)"},
			{Kind: values.BlockPlaceholder, Lines: []string{"Вставьте измерения своей установки"}},
			{Kind: values.BlockTable, Table: values.Table{Rows: [][]string{{"g", "9,81"}}}},
			{Kind: values.BlockCaption, Lines: []string{"Рисунок 2 – Схема установки"}},
			{Kind: values.BlockBibliography, Lines: []string{
				"Сивухин Д. В. Общий курс физики. Т. 1. – М. : Физматлит, 2005. – 560 с.",
				"Ландау Л. Д., Лифшиц Е. М. Механика. – М. : Наука, 1988. – 216 с.",
			}},
		},
	}
}

func part(t *testing.T, doc values.Document, name string) string {
	t.Helper()
	parts, _ := entries(t, render(t, doc))
	p, ok := parts[name]
	if !ok {
		t.Fatalf("no part %s", name)
	}
	return p
}

// elements returns the names of the start elements of an XML part with their attributes, in order.
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
			t.Fatalf("not well-formed: %v", err)
		}
		if el, ok := tok.(xml.StartElement); ok {
			e := el.Name.Local
			for _, a := range el.Attr {
				e += " " + a.Name.Local + "=" + a.Value
			}
			out = append(out, e)
		}
	}
}

func TestPartsAreDeclaredAndLinked(t *testing.T) {
	parts, names := entries(t, render(t, paper()))
	for name, p := range parts {
		wellFormed(t, name, p)
	}
	var types struct {
		Overrides []struct {
			PartName string `xml:"PartName,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal([]byte(parts["[Content_Types].xml"]), &types); err != nil {
		t.Fatal(err)
	}
	var overridden []string
	for _, o := range types.Overrides {
		overridden = append(overridden, strings.TrimPrefix(o.PartName, "/"))
	}
	var rels struct {
		Rels []rel `xml:"Relationship"`
	}
	if err := xml.Unmarshal([]byte(parts["word/_rels/document.xml.rels"]), &rels); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range rels.Rels {
		if ids[r.ID] {
			t.Errorf("duplicate relationship %s", r.ID)
		}
		ids[r.ID] = true
		if !slices.Contains(names, "word/"+r.Target) {
			t.Errorf("relationship %s points at a missing part %s", r.ID, r.Target)
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".rels") || name == "[Content_Types].xml" {
			continue
		}
		if !slices.Contains(overridden, name) {
			t.Errorf("part %s has no content type override", name)
		}
		if name != "word/document.xml" && !ids[relTo(rels.Rels, strings.TrimPrefix(name, "word/"))] {
			t.Errorf("part %s is not linked from the document", name)
		}
	}
	for _, o := range overridden {
		if !slices.Contains(names, o) {
			t.Errorf("content type override for a missing part %s", o)
		}
	}
	for _, id := range []string{footerRel, firstFooterRel} {
		if !strings.Contains(parts["word/document.xml"], `r:id="`+id+`"`) {
			t.Errorf("the section does not reference %s", id)
		}
	}
}

type rel struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

// relTo returns the id of the relationship to target, empty if none.
func relTo(rels []rel, target string) string {
	for _, r := range rels {
		if r.Target == target {
			return r.ID
		}
	}
	return ""
}

func TestLengthConversions(t *testing.T) {
	cases := []struct {
		name      string
		got, want int
	}{
		{"30 mm", twipsFromMM(30), 1701},
		{"15 mm", twipsFromMM(15), 850},
		{"20 mm", twipsFromMM(20), 1134},
		{"1.25 cm", twipsFromTenthMM(125), 709},
		{"25.4 mm is one inch", twipsFromTenthMM(254), 1440},
		{"0 mm", twipsFromMM(0), 0},
		{"line 1.5", lineValue(150), 360},
		{"line 1", lineValue(100), 240},
		{"line 1.15", lineValue(115), 276},
		{"line 2", lineValue(200), 480},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestStylesFollowTheProfile(t *testing.T) {
	cases := []struct {
		name   string
		format values.Formatting
		want   []string // in styles.xml, then in document.xml after "|"
	}{
		{"GOST defaults", values.Formatting{}, []string{
			`w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="Times New Roman" w:cs="Times New Roman"`,
			`<w:sz w:val="28"/><w:szCs w:val="28"/><w:lang w:val="ru-RU"/>`,
			`<w:spacing w:after="0" w:line="360" w:lineRule="auto"/>`,
			`w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/><w:pPr><w:widowControl/><w:ind w:firstLine="709"/><w:jc w:val="both"/>`,
			// Headings in the body size, bold (GOST 7.32).
			`w:styleId="Heading1"><w:name w:val="heading 1"/>`, `<w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/>`,
			`<w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/>`, `<w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:i/><w:sz w:val="28"/>`,
			`w:styleId="TOC1"><w:name w:val="toc 1"/>`, `w:pos="9355"`, // 11906 - 1701 - 850
			`w:styleId="Caption"><w:name w:val="caption"/>`, `w:styleId="TableGrid"`, `<w:insideV w:val="single"`,
			`|<w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1701" w:header="709" w:footer="709" w:gutter="0"/>`,
		}},
		{"custom profile", values.Formatting{
			FontName: "PT Astra Serif", FontSizePt: 12, LineSpacingPct: 100,
			MarginTopMM: 25, MarginRightMM: 10, MarginBottomMM: 25, MarginLeftMM: 35, IndentTenthMM: 100,
		}, []string{
			`w:ascii="PT Astra Serif" w:hAnsi="PT Astra Serif"`, `<w:sz w:val="24"/><w:szCs w:val="24"/>`,
			`<w:spacing w:after="0" w:line="240" w:lineRule="auto"/>`, `<w:ind w:firstLine="567"/>`,
			`<w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/>`,
			`w:pos="9355"`, // 11906 - 1984 - 567 (35 mm, 10 mm)
			`|<w:pgMar w:top="1417" w:right="567" w:bottom="1417" w:left="1984" w:header="709" w:footer="709"`,
		}},
		{"invalid values fall back to GOST", values.Formatting{
			FontName: `Arial"/><w:b/><w:x a="`, FontSizePt: 2, LineSpacingPct: 1000, MarginLeftMM: -3, IndentTenthMM: 9999,
		}, []string{
			`w:ascii="Times New Roman"`, `<w:sz w:val="28"/>`, `w:line="360"`, `<w:ind w:firstLine="709"/>`,
			`|w:left="1701"`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := paper()
			doc.Format = tc.format
			parts, _ := entries(t, render(t, doc))
			for _, w := range tc.want {
				name, s := "word/styles.xml", w
				if after, ok := strings.CutPrefix(w, "|"); ok {
					name, s = "word/document.xml", after
				}
				if !strings.Contains(parts[name], s) {
					t.Errorf("%s lacks %s", name, s)
				}
			}
			if strings.Contains(parts["word/styles.xml"], "Arial") || strings.Count(parts["word/styles.xml"], "<w:x") > 0 {
				t.Error("an invalid font name reached the styles")
			}
			// Numbering indents follow the profile too.
			ind := fmt.Sprintf(`<w:ind w:left="0" w:firstLine="%d"/>`, twipsFromTenthMM(doc.Format.Normalized().IndentTenthMM))
			if !strings.Contains(parts["word/numbering.xml"], ind) {
				t.Errorf("numbering lacks %s", ind)
			}
		})
	}
}

func TestAttrEscapes(t *testing.T) {
	if got := attr(`a"b'c<d>&e` + "\n\x01"); got != "a&#34;b&#39;c&lt;d&gt;&amp;e&#xA;" {
		t.Fatalf("attr = %q", got)
	}
}

// Hostile text in every field of every block kind changes no element or attribute: the structure equals that of the
// same document with plain text.
func TestHostileTextInEveryBlockKind(t *testing.T) {
	ctl := string(rune(1)) + string(rune(0x1b)) + string(rune(0xFFFE)) + string(rune(0x202E)) + string(rune(0x2066))
	evil := `</w:t></w:r><w:r><w:t>x</w:t><w:fldSimple w:instr="PAGE"/>` + " & \"q\" 'a' <b> ]]> <!-- " + ctl + "\xff"
	clean := `</w:t></w:r><w:r><w:t>x</w:t><w:fldSimple w:instr="PAGE"/> & "q" 'a' <b> ]]> <!-- `
	build := func(s string) values.Document {
		return values.Document{
			Title:           values.TitlePage{University: s, Faculty: s, Heading: s, Topic: s, Performer: s, Checker: s, Place: s},
			TableOfContents: true,
			Blocks: []values.Block{
				{Kind: values.BlockHeading, Level: 1, Lines: []string{s}},
				{Kind: values.BlockParagraph, Lines: []string{s, s}},
				{Kind: values.BlockBulletList, Lines: []string{s}},
				{Kind: values.BlockNumberedList, Lines: []string{s}},
				{Kind: values.BlockTable, Caption: s, Table: values.Table{Header: []string{s, s}, Rows: [][]string{{s, s}}}},
				{Kind: values.BlockCaption, Lines: []string{s}},
				{Kind: values.BlockBibliography, Lines: []string{s}},
				{Kind: values.BlockPlaceholder, Lines: []string{s}, Caption: s},
			},
		}
	}
	h := build(evil)
	h.Format.FontName = evil // falls back to the default, as the plain document has
	hostile, _ := entries(t, render(t, h))
	plain, _ := entries(t, render(t, build("x")))
	for name := range plain {
		if !slices.Equal(elements(t, hostile[name]), elements(t, plain[name])) {
			t.Errorf("%s: hostile text changed the structure", name)
		}
	}
	got := texts(t, hostile["word/document.xml"])
	want := 0
	for _, s := range got {
		if strings.Contains(s, "x") && s != clean && !strings.HasSuffix(s, clean) {
			t.Errorf("text %q is not the cleaned input", s)
		}
		if strings.Contains(s, clean) {
			want++
		}
	}
	// 7 title lines, the TOC entry, the heading, 2 lines, 2 items, 2 header cells, 2 cells, the table caption, the
	// caption, the entry, the placeholder and its figure caption.
	if want != 7+1+1+2+2+2+2+1+1+1+1+1 {
		t.Errorf("%d texts carry the cleaned input:\n%q", want, got)
	}
}

func TestTableOfContents(t *testing.T) {
	parts, _ := entries(t, render(t, paper()))
	doc := parts["word/document.xml"]
	if !strings.Contains(parts["word/settings.xml"], `<w:updateFields w:val="true"/>`) {
		t.Fatal("Word is not asked to update the TOC on open")
	}
	if !strings.Contains(doc, `<w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText>`) {
		t.Fatal("no TOC field")
	}
	if strings.Count(doc, `w:fldCharType="begin"`) != 1 || strings.Count(doc, `w:fldCharType="end"`) != 1 {
		t.Fatal("the TOC field is not closed once")
	}
	want := []struct {
		style, text string
	}{
		{"TOC1", "Введение"}, {"TOC1", "Теория"}, {"TOC2", "Период колебаний"}, {"TOC3", "Погрешности"},
		{"TOC1", values.BibliographyHeading},
	}
	at := strings.Index(doc, values.ContentsHeading)
	for i, w := range want {
		entry := fmt.Sprintf(`<w:pStyle w:val="%s"/></w:pPr>`, w.style)
		link := fmt.Sprintf(`<w:hyperlink w:anchor="_Toc%d" w:history="1"><w:r><w:t xml:space="preserve">%s</w:t>`, i+1, w.text)
		next := strings.Index(doc[at:], link)
		if next < 0 || !strings.Contains(doc[at:at+next], entry) {
			t.Fatalf("TOC entry %d (%s %s) is missing or out of order", i+1, w.style, w.text)
		}
		at += next
		target := fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="_Toc%d"/><w:r><w:t xml:space="preserve">%s</w:t></w:r><w:bookmarkEnd w:id="%d"/>`, i+1, i+1, w.text, i+1)
		if !strings.Contains(doc, target) {
			t.Errorf("heading %s is not the target of its entry", w.text)
		}
	}
	if end, first := strings.Index(doc, `w:fldCharType="end"`), strings.Index(doc, "_Toc1\"/>"); end > first {
		t.Fatal("the TOC field ends after the body starts")
	}

	// Without a TOC: no field, no bookmarks, no update on open.
	plain := paper()
	plain.TableOfContents = false
	parts, _ = entries(t, render(t, plain))
	if strings.Contains(parts["word/document.xml"], "TOC") || strings.Contains(parts["word/document.xml"], "bookmark") ||
		strings.Contains(parts["word/settings.xml"], "updateFields") || strings.Contains(parts["word/document.xml"], values.ContentsHeading) {
		t.Fatal("a document without a TOC has one")
	}

	// A TOC without headings is an empty, closed field.
	empty := values.Document{TableOfContents: true, Blocks: []values.Block{{Kind: values.BlockParagraph, Lines: []string{"текст"}}}}
	d := part(t, empty, "word/document.xml")
	if !strings.Contains(d, `w:fldCharType="separate"/></w:r><w:r><w:fldChar w:fldCharType="end"/>`) {
		t.Fatal("an empty TOC is not a closed field")
	}
}

func TestEveryNumberedListRestarts(t *testing.T) {
	parts, _ := entries(t, render(t, paper()))
	num := parts["word/numbering.xml"]
	for _, s := range []string{
		`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="singleLevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:suff w:val="space"/><w:lvlText w:val="–"/>`,
		`<w:numFmt w:val="decimal"/><w:suff w:val="space"/><w:lvlText w:val="%1)"/>`,
		`<w:numFmt w:val="decimal"/><w:suff w:val="space"/><w:lvlText w:val="%1."/>`,
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`,
		`<w:num w:numId="2"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`,
		`<w:num w:numId="3"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`,
		`<w:num w:numId="4"><w:abstractNumId w:val="2"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`,
	} {
		if !strings.Contains(num, s) {
			t.Errorf("numbering lacks %s", s)
		}
	}
	if strings.Contains(num, `w:numId="5"`) {
		t.Error("an instance without a list")
	}
	doc := parts["word/document.xml"]
	for _, c := range []struct {
		id    int
		items []string
	}{
		{1, []string{"математический маятник", "физический маятник"}},
		{2, []string{"измерить длину", "засечь время"}},
		{3, []string{"систематические", "случайные"}},
		{4, []string{"Сивухин Д. В. Общий курс физики. Т. 1. – М. : Физматлит, 2005. – 560 с."}},
	} {
		for _, item := range c.items {
			s := fmt.Sprintf(`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="%d"/></w:numPr></w:pPr><w:r><w:t xml:space="preserve">%s`, c.id, item)
			if !strings.Contains(doc, s) {
				t.Errorf("item %q is not in list %d", item, c.id)
			}
		}
	}
}

func TestTables(t *testing.T) {
	doc := part(t, paper(), "word/document.xml")
	first := strings.Index(doc, "Таблица 1 – Результаты измерений")
	second := strings.Index(doc, `<w:t xml:space="preserve">Таблица 2</w:t>`)
	tbls := strings.Count(doc, "<w:tbl>")
	if first < 0 || second < 0 || tbls != 2 {
		t.Fatalf("captions at %d, %d; %d tables", first, second, tbls)
	}
	tbl := doc[strings.Index(doc, "<w:tbl>"):strings.Index(doc, "</w:tbl>")]
	if first > strings.Index(doc, "<w:tbl>") {
		t.Fatal("the caption is not above the table")
	}
	if !strings.Contains(doc, `<w:pStyle w:val="Caption"/><w:keepNext/><w:jc w:val="left"/></w:pPr><w:r><w:t xml:space="preserve">Таблица 1`) {
		t.Fatal("the table caption is not a left caption kept with its table")
	}
	if n := strings.Count(tbl, "<w:tr>"); n != 4 {
		t.Fatalf("rows = %d, want the header and 3 rows", n)
	}
	if n := strings.Count(tbl, "<w:tc>"); n != 8 {
		t.Fatalf("cells = %d, want 4 rows of 2 (the short row padded)", n)
	}
	if n := strings.Count(tbl, `<w:gridCol w:w="`); n != 2 || !strings.Contains(tbl, `<w:gridCol w:w="4677"/><w:gridCol w:w="4678"/>`) {
		t.Fatalf("grid = %d columns: %s", n, tbl[:min(len(tbl), 400)])
	}
	if strings.Count(tbl, "<w:tblHeader/>") != 1 ||
		!strings.Contains(tbl, `<w:trPr><w:tblHeader/></w:trPr><w:tc><w:tcPr><w:tcW w:w="4677" w:type="dxa"/></w:tcPr><w:p><w:pPr><w:pStyle w:val="TableText"/></w:pPr><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Длина, м`) {
		t.Fatal("the header row is not repeated and bold")
	}
	if !strings.Contains(tbl, "<w:tblBorders>") {
		t.Fatal("the table has no borders")
	}

	// A table without a header, as the last block, is followed by a paragraph.
	last := values.Document{Blocks: []values.Block{{Kind: values.BlockTable, Table: values.Table{Rows: [][]string{{"a", "b", "c"}}}}}}
	d := part(t, last, "word/document.xml")
	if strings.Contains(d, "tblHeader") || !strings.Contains(d, `</w:tbl><w:p/><w:sectPr>`) || strings.Count(d, "<w:gridCol") != 3 {
		t.Fatal("a last table without a header is rendered wrong")
	}
}

func TestPageNumbersAndPages(t *testing.T) {
	parts, _ := entries(t, render(t, paper()))
	ftr := parts["word/footer1.xml"]
	if !strings.Contains(ftr, `<w:jc w:val="center"/>`) || !strings.Contains(ftr, `<w:instrText xml:space="preserve"> PAGE </w:instrText>`) {
		t.Fatal("the footer has no centred PAGE field")
	}
	if strings.Contains(parts["word/footer2.xml"], "PAGE") {
		t.Fatal("the title page footer has a number")
	}
	doc := parts["word/document.xml"]
	sect := doc[strings.Index(doc, "<w:sectPr>"):]
	if !strings.Contains(sect, `<w:footerReference w:type="default" r:id="rId4"/><w:footerReference w:type="first" r:id="rId5"/>`) ||
		!strings.HasSuffix(sect, `<w:titlePg/></w:sectPr></w:body></w:document>`) {
		t.Fatalf("section = %s", sect)
	}

	// Level-1 headings start pages: not the first one (the TOC ends with a page break), but the next one and the
	// bibliography; other levels never.
	for _, c := range []struct {
		text    string
		newPage bool
	}{
		{"Введение", false}, {"Теория", true}, {"Период колебаний", false}, {values.BibliographyHeading, true},
	} {
		i := strings.Index(doc, `<w:t xml:space="preserve">`+c.text+`</w:t></w:r><w:bookmarkEnd`)
		p := doc[strings.LastIndex(doc[:i], "<w:p>"):i]
		if got := strings.Contains(p, "<w:pageBreakBefore/>"); got != c.newPage {
			t.Errorf("%s: page break before = %v", c.text, got)
		}
	}
	if strings.Count(doc, `<w:br w:type="page"/>`) != 2 {
		t.Error("want page breaks after the title page and after the TOC")
	}

	// Without a title page: numbers from the first page.
	noTitle := paper()
	noTitle.Title = values.TitlePage{}
	d := part(t, noTitle, "word/document.xml")
	if strings.Contains(d, "titlePg") || strings.Contains(d, `w:type="first"`) {
		t.Fatal("a document without a title page hides the first number")
	}

	// Without SectionsOnNewPage («Сохранить в Word»): no heading starts a page, with or without a title page or TOC.
	for _, doc := range []values.Document{paper(), noTitle, sample()} {
		doc.SectionsOnNewPage = false
		if d := part(t, doc, "word/document.xml"); strings.Contains(d, "pageBreakBefore") {
			t.Fatal("a heading starts a new page without SectionsOnNewPage")
		}
	}
}

func TestPlaceholdersAreVisible(t *testing.T) {
	doc := part(t, paper(), "word/document.xml")
	for _, s := range []string{
		`<w:highlight w:val="yellow"/></w:rPr><w:t xml:space="preserve">▢ Вставьте график зависимости периода от длины</w:t>`,
		`<w:highlight w:val="yellow"/></w:rPr><w:t xml:space="preserve">▢ Вставьте измерения своей установки</w:t>`,
		`<w:pStyle w:val="Caption"/><w:jc w:val="center"/></w:pPr><w:r><w:t xml:space="preserve">Рисунок 1 – Зависимость T(l)</w:t>`,
		`<w:t xml:space="preserve">Рисунок 2 – Схема установки</w:t>`, // a caption block is written as is
	} {
		if !strings.Contains(doc, s) {
			t.Errorf("document lacks %s", s)
		}
	}
	if strings.Count(doc, "Рисунок ") != 2 {
		t.Error("a placeholder without a figure name got a caption")
	}
}

func TestOddInputDoesNotBreakTheFile(t *testing.T) {
	docs := []values.Document{
		{Blocks: []values.Block{{Kind: values.BlockHeading}}},
		{Blocks: []values.Block{{Kind: values.BlockHeading, Level: 99, Lines: []string{"x"}}, {Kind: values.BlockHeading, Level: -1, Lines: []string{"y"}}}},
		{TableOfContents: true, Blocks: []values.Block{{Kind: values.BlockHeading, Lines: []string{""}}}},
		{TableOfContents: true},
		{Title: values.TitlePage{Place: "2026"}, TableOfContents: true},
		{Blocks: []values.Block{{Kind: values.BlockBulletList}, {Kind: values.BlockNumberedList}, {Kind: values.BlockBibliography}}},
		{Blocks: []values.Block{{Kind: values.BlockPlaceholder}, {Kind: values.BlockCaption}, {Kind: values.BlockParagraph}}},
		{Blocks: []values.Block{{Kind: values.BlockTable, Table: values.Table{Header: []string{"a"}, Rows: [][]string{{}, {"1", "2", "3"}}}}}},
		{Blocks: []values.Block{{Kind: values.BlockTable, Table: values.Table{Header: []string{"a"}}}}},
	}
	for i, d := range docs {
		parts, _ := entries(t, render(t, d))
		for name, p := range parts {
			if _, err := xmlTokens(p); err != nil {
				t.Errorf("document %d: %s: %v", i, name, err)
			}
		}
	}
	d := part(t, docs[1], "word/document.xml")
	if !strings.Contains(d, `"Heading3"`) || !strings.Contains(d, `"Heading1"`) {
		t.Fatal("heading levels are not clamped")
	}
}

func xmlTokens(s string) (int, error) {
	dec := xml.NewDecoder(strings.NewReader(s))
	n := 0
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
	}
}

func TestOversizedDocumentsAreRefused(t *testing.T) {
	cases := []values.Document{
		{Blocks: make([]values.Block, values.MaxDocumentBlocks+1)},
		{Blocks: []values.Block{{Kind: "macro", Lines: []string{"x"}}}},
		{Blocks: []values.Block{{Kind: values.BlockTable, Table: values.Table{Rows: make([][]string, values.MaxTableRows+1)}}}},
		{Blocks: []values.Block{{Kind: values.BlockTable, Table: values.Table{Rows: [][]string{make([]string, values.MaxTableColumns+1)}}}}},
		{Blocks: []values.Block{{Kind: values.BlockTable}}},
		{Blocks: []values.Block{{Kind: values.BlockParagraph, Lines: []string{strings.Repeat("a", values.MaxTextRunes+1)}}}},
		// Empty cells cost no characters, but rows are padded to the widest one: refused by the cell count.
		{Blocks: slices.Repeat([]values.Block{{Kind: values.BlockTable, Table: values.Table{
			Header: make([]string, values.MaxTableColumns), Rows: make([][]string, values.MaxTableRows-1),
		}}}, values.MaxDocumentBlocks)},
		// Empty list items cost no characters either: refused by the item count.
		{Blocks: slices.Repeat([]values.Block{{Kind: values.BlockBulletList, Lines: make([]string, values.MaxBlockLines)}}, values.MaxDocumentBlocks)},
	}
	for i, d := range cases {
		b, err := New().Render(context.Background(), d)
		if !errors.Is(err, ErrRender) || !errors.Is(err, values.ErrInvalidDocument) || b != nil {
			t.Errorf("document %d: err = %v", i, err)
		}
	}
}

// A document at every structure limit, with no text at all, still renders to a bounded size.
func TestStructureAtTheLimitsStaysSmall(t *testing.T) {
	const maxDocumentXML = 16 << 20
	table := values.Block{Kind: values.BlockTable, Table: values.Table{
		Header: make([]string, values.MaxTableColumns), Rows: make([][]string, values.MaxTableRows-1),
	}}
	list := values.Block{Kind: values.BlockBulletList, Lines: make([]string, values.MaxBlockLines)}
	blocks := slices.Repeat([]values.Block{table}, values.MaxDocumentCells/(values.MaxTableRows*values.MaxTableColumns))
	blocks = append(blocks, slices.Repeat([]values.Block{list}, values.MaxDocumentItems/values.MaxBlockLines)...)
	blocks = append(blocks, slices.Repeat([]values.Block{{Kind: values.BlockBibliography}}, values.MaxDocumentBlocks-len(blocks))...)
	doc := values.Document{TableOfContents: true, SectionsOnNewPage: true, Blocks: blocks}
	n := len(part(t, doc, "word/document.xml"))
	if n > maxDocumentXML {
		t.Fatalf("document.xml = %d bytes, want at most %d", n, maxDocumentXML)
	}
	t.Logf("document.xml = %d bytes", n)
}

func TestPaperIsDeterministic(t *testing.T) {
	if !bytes.Equal(render(t, paper()), render(t, paper())) {
		t.Fatal("two renders of the same paper differ")
	}
}

// The sample for the manual check (docs/qa/run-alpha.md, TASK-104): always rendered into a temporary directory, and
// into PARATECH_DOCX_SAMPLE_DIR when it is set.
func TestSamplePaper(t *testing.T) {
	b := render(t, paper())
	dirs := []string{t.TempDir()}
	if dir := os.Getenv("PARATECH_DOCX_SAMPLE_DIR"); dir != "" {
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "paratech-gost-sample.docx")
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(b) {
			t.Fatalf("sample %s not written: %v", path, err)
		}
		t.Logf("sample: %s", path)
	}
	parts, _ := entries(t, b)
	for name, p := range parts {
		wellFormed(t, name, p)
	}
}
