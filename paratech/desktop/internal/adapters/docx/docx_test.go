package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

func sample() values.Document {
	return values.Document{
		Title: values.TitlePage{
			University: "МГУ", Faculty: "Факультет ВМК", Heading: "Домашнее задание по дисциплине «Анализ»",
			Topic: "Пределы", Performer: "Выполнил: студент группы ИВТ-21 Иванов И. И.", Checker: "Проверил: Петров П. П.",
			Place: "Москва 2026",
		},
		Blocks: []values.Block{
			{Kind: values.BlockHeading, Level: 1, Lines: []string{"Решение"}},
			{Kind: values.BlockParagraph, Lines: []string{"Строка 1", "Строка 2"}},
			{Kind: values.BlockHeading, Level: 2, Lines: []string{"Задача 2"}},
		},
	}
}

func render(t *testing.T, doc values.Document) []byte {
	t.Helper()
	b, err := New().Render(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// entries opens the package and returns every entry by name.
func entries(t *testing.T, b []byte) (map[string]string, []string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip archive: %v", err)
	}
	out := map[string]string{}
	var names []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(data)
		names = append(names, f.Name)
	}
	return out, names
}

// wellFormed parses an XML part to the end.
func wellFormed(t *testing.T, name, part string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(part))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("%s is not well-formed XML: %v\n%s", name, err, part)
		}
	}
}

// texts returns the character data of the w:t elements of document.xml, in order.
func texts(t *testing.T, documentXML string) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(documentXML))
	var out []string
	inText := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			inText = v.Name.Local == "t"
		case xml.EndElement:
			inText = false
		case xml.CharData:
			if inText {
				out = append(out, string(v))
			}
		}
	}
}

func TestPackageStructure(t *testing.T) {
	parts, names := entries(t, render(t, sample()))
	want := []string{
		"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/_rels/document.xml.rels", "word/styles.xml",
		"word/settings.xml", "word/numbering.xml", "word/footer1.xml", "word/footer2.xml",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for name, part := range parts {
		wellFormed(t, name, part)
	}
	for _, s := range []string{
		`PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"`,
		`PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"`,
		`Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"`,
	} {
		if !strings.Contains(parts["[Content_Types].xml"], s) {
			t.Errorf("content types lack %s", s)
		}
	}
	if !strings.Contains(parts["_rels/.rels"], `relationships/officeDocument" Target="word/document.xml"`) ||
		!strings.Contains(parts["word/_rels/document.xml.rels"], `relationships/styles" Target="styles.xml"`) {
		t.Fatal("relationships do not point at the document and its styles")
	}
	for _, id := range []string{"Heading1", "Heading2", "Heading3"} {
		if !strings.Contains(parts["word/styles.xml"], `w:styleId="`+id+`"`) {
			t.Errorf("styles lack %s", id)
		}
	}
}

func TestTitlePageThenBody(t *testing.T) {
	parts, _ := entries(t, render(t, sample()))
	doc := parts["word/document.xml"]
	got := texts(t, doc)
	want := []string{
		"МГУ", "Факультет ВМК", "Домашнее задание по дисциплине «Анализ»", "Пределы",
		"Выполнил: студент группы ИВТ-21 Иванов И. И.", "Проверил: Петров П. П.", "Москва 2026",
		"Решение", "Строка 1", "Строка 2", "Задача 2",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("texts =\n%q\nwant\n%q", got, want)
	}
	brk := strings.Index(doc, `<w:br w:type="page"/>`)
	if brk < 0 || brk > strings.Index(doc, "Решение") || brk < strings.Index(doc, "Москва 2026") {
		t.Fatal("the page break does not separate the title page from the text")
	}
	if !strings.Contains(doc, `<w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t xml:space="preserve">Решение`) ||
		!strings.Contains(doc, `<w:pStyle w:val="Heading2"/>`) {
		t.Fatal("headings lack their styles")
	}
	if !strings.Contains(doc, `Строка 1</w:t><w:br/><w:t xml:space="preserve">Строка 2`) {
		t.Fatal("the lines of a paragraph are not broken")
	}
}

// A title page without a body ends the document: no page break, so no blank second page.
func TestTitlePageWithoutBlocks(t *testing.T) {
	doc := sample()
	doc.Blocks = nil
	parts, _ := entries(t, render(t, doc))
	if strings.Contains(parts["word/document.xml"], `<w:br w:type="page"/>`) {
		t.Fatal("a title page without a body ends with a page break")
	}
	if got := texts(t, parts["word/document.xml"]); len(got) == 0 || got[0] != "МГУ" {
		t.Fatalf("texts = %q", got)
	}
}

// Missing lines of the title page leave no empty paragraph or label; no title page means no page break.
func TestTitlePageWithMissingFields(t *testing.T) {
	doc := sample()
	doc.Title = values.TitlePage{Heading: "Домашнее задание", Place: "2026"}
	parts, _ := entries(t, render(t, doc))
	got := texts(t, parts["word/document.xml"])
	if !slices.Equal(got[:2], []string{"Домашнее задание", "2026"}) || strings.Contains(parts["word/document.xml"], "Проверил") {
		t.Fatalf("texts = %q", got)
	}
	if n := strings.Count(parts["word/document.xml"], "<w:p>"); n != 2+1+3 {
		t.Fatalf("paragraphs = %d, want 2 title lines, the page break and 3 blocks", n)
	}

	doc.Title = values.TitlePage{}
	parts, _ = entries(t, render(t, doc))
	if strings.Contains(parts["word/document.xml"], `w:type="page"`) {
		t.Fatal("a page break without a title page")
	}
}

func TestTextIsEscaped(t *testing.T) {
	ctl := string(rune(1)) + string(rune(0x1b)) + string(rune(0xFFFE))
	evil := `</w:t></w:r><w:r><w:t>x</w:t>` + " & \"q\" 'a' <b>" + ctl + "\xff"
	doc := values.Document{
		Title:  values.TitlePage{University: evil},
		Blocks: []values.Block{{Kind: values.BlockParagraph, Lines: []string{evil, "a\tb"}}},
	}
	parts, _ := entries(t, render(t, doc))
	wellFormed(t, "document", parts["word/document.xml"])
	got := texts(t, parts["word/document.xml"])
	clean := `</w:t></w:r><w:r><w:t>x</w:t> & "q" 'a' <b>`
	if len(got) != 4 || got[0] != clean || got[1] != clean || got[2] != "a" || got[3] != "b" {
		t.Fatalf("texts = %q", got)
	}
	if !strings.Contains(parts["word/document.xml"], `a</w:t><w:tab/><w:t xml:space="preserve">b`) {
		t.Fatal("a tab is not a tab element")
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	if !bytes.Equal(render(t, sample()), render(t, sample())) {
		t.Fatal("two renders of the same document differ")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Render(ctx, sample()); !errors.Is(err, ErrRender) {
		t.Fatalf("cancelled: err = %v", err)
	}
}
