// Package docx renders a homework document (values.Document) as a Word file (TASK-094): a minimal Office Open XML
// package of [Content_Types].xml, the relationships, word/document.xml and word/styles.xml, zipped with archive/zip.
// Standard library only; Word and LibreOffice open it. The output is deterministic: the same document gives the same
// bytes (an email attachment is pinned by its digest).
//
// Every text is data: characters that XML cannot hold are dropped and the rest is escaped, so a text cannot add
// markup. The page is A4 with the margins usual for Russian student papers (left 30 mm, right 15 mm, top and bottom
// 20 mm), Times New Roman 14 pt, one and a half line spacing.
package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

// ErrRender means the package could not be assembled.
var ErrRender = errors.New("docx: the document cannot be rendered")

// Renderer renders documents. The zero value is ready to use.
type Renderer struct{}

// New returns a Renderer.
func New() Renderer { return Renderer{} }

// Render returns the .docx bytes of doc.
func (Renderer) Render(ctx context.Context, doc values.Document) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"word/document.xml", document(doc)},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", styles()},
	}
	// One fixed modification time for every entry keeps the output deterministic.
	modified := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRender, err)
		}
		if _, err := w.Write([]byte(p.content)); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRender, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	return buf.Bytes(), nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const contentTypes = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`</Types>`

const packageRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

const documentRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

const wordNS = `http://schemas.openxmlformats.org/wordprocessingml/2006/main`

// styles: the defaults (Times New Roman 14 pt, Russian, 1.5 lines) and three heading levels; the built-in names
// ("heading 1", ...) make Word list the headings in its navigation pane.
func styles() string {
	return xmlHeader + `<w:styles xmlns:w="` + wordNS + `">` +
		`<w:docDefaults><w:rPrDefault><w:rPr>` +
		`<w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="Times New Roman" w:cs="Times New Roman"/>` +
		`<w:sz w:val="28"/><w:szCs w:val="28"/><w:lang w:val="ru-RU"/>` +
		`</w:rPr></w:rPrDefault>` +
		`<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
		headingStyle(1, 32, "") + headingStyle(2, 28, "") + headingStyle(3, 28, "<w:i/>") +
		`</w:styles>`
}

func headingStyle(level, size int, extra string) string {
	return fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/>`+
		`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>`+
		`<w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="%d"/></w:pPr>`+
		`<w:rPr><w:b/>%s<w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`, level, level, level-1, extra, size, size)
}

// Layout of the page and of the title page; lengths in twentieths of a point, sizes in half points.
const (
	sectPr = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1701" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr>`
	bodyParagraph = `<w:ind w:firstLine="709"/><w:jc w:val="both"/>`
	pageBreak     = `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`

	headingGap = 3600 // before the heading of the title page
	topicGap   = 120
	signGap    = 2400 // before «Выполнил»
	placeGap   = 2400 // before the city and the year
	titleSize  = 32
	center     = "center"
	right      = "right"
)

// document renders word/document.xml.
func document(doc values.Document) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:document xmlns:w="` + wordNS + `"><w:body>`)
	if !doc.Title.IsZero() {
		titlePage(&b, doc.Title, len(doc.Blocks) > 0)
	}
	for _, bl := range doc.Blocks {
		block(&b, bl)
	}
	b.WriteString(sectPr + `</w:body></w:document>`)
	return b.String()
}

// titlePage writes the lines of the title page that are set, then a page break when a body follows (no blank page).
func titlePage(b *strings.Builder, t values.TitlePage, body bool) {
	lines := []struct {
		text   string
		align  string
		before int
		bold   bool
		size   int // 0: the default
	}{
		{t.University, center, 0, false, 0},
		{t.Faculty, center, 0, false, 0},
		{t.Heading, center, headingGap, true, titleSize},
		{t.Topic, center, topicGap, false, 0},
		{t.Performer, right, signGap, false, 0},
		{t.Checker, right, 0, false, 0},
		{t.Place, center, placeGap, false, 0},
	}
	for _, l := range lines {
		if l.text == "" {
			continue
		}
		fmt.Fprintf(b, `<w:p><w:pPr><w:spacing w:before="%d"/><w:jc w:val="%s"/></w:pPr><w:r>`, l.before, l.align)
		if l.bold || l.size != 0 {
			b.WriteString(`<w:rPr>`)
			if l.bold {
				b.WriteString(`<w:b/>`)
			}
			if l.size != 0 {
				fmt.Fprintf(b, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, l.size, l.size)
			}
			b.WriteString(`</w:rPr>`)
		}
		text(b, l.text)
		b.WriteString(`</w:r></w:p>`)
	}
	if body {
		b.WriteString(pageBreak)
	}
}

// block writes a heading, or a paragraph whose lines are separated by line breaks.
func block(b *strings.Builder, bl values.Block) {
	if bl.Kind == values.BlockHeading {
		level := min(max(bl.Level, values.MinHeadingLevel), values.MaxHeadingLevel)
		fmt.Fprintf(b, `<w:p><w:pPr><w:pStyle w:val="Heading%d"/></w:pPr><w:r>`, level)
		text(b, strings.Join(bl.Lines, " "))
		b.WriteString(`</w:r></w:p>`)
		return
	}
	b.WriteString(`<w:p><w:pPr>` + bodyParagraph + `</w:pPr><w:r>`)
	for i, line := range bl.Lines {
		if i > 0 {
			b.WriteString(`<w:br/>`)
		}
		text(b, line)
	}
	b.WriteString(`</w:r></w:p>`)
}

// text writes s as the text of a run: tabs become tab elements, line breaks become spaces, and the characters XML
// cannot hold are dropped before the rest is escaped.
func text(b *strings.Builder, s string) {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(xmlChars(s))
	for i, part := range strings.Split(s, "\t") {
		if i > 0 {
			b.WriteString(`<w:tab/>`)
		}
		if part == "" {
			continue
		}
		b.WriteString(`<w:t xml:space="preserve">`)
		_ = xml.EscapeText(b, []byte(part)) // a strings.Builder never fails a write
		b.WriteString(`</w:t>`)
	}
}

// xmlChars keeps the characters XML 1.0 allows: tab, the line breaks and the ranges of its Char production.
func xmlChars(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r >= 0x20 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
			return r
		}
		return -1
	}, strings.ToValidUTF8(s, ""))
}
