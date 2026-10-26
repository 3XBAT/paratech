package pdf

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

// ErrRender means the print page could not be built: the document fails values.Document.Validate or has a block the
// renderer does not know.
var ErrRender = errors.New("pdf: the document cannot be rendered")

// Layout numbers of adapters/docx (body.go, styles.go, parts.go), in twips, so the PDF matches the .docx; adapters do
// not import each other (ARCH-30), hence the copy.
const (
	headingGap    = 3600 // before the heading of the title page
	topicGap      = 120
	signGap       = 2400 // before «Выполнил»
	placeGap      = 2400 // before the city and the year
	titleSizePt   = 16   // the heading of the title page
	headingBefore = 240
	headingAfter  = 120
	tocIndent     = 283 // per TOC level
	tabStop       = 709 // the default tab stop
	cellPadding   = 108 // left and right cell margin
	borderPt      = 0.5 // w:sz="4": eighths of a point
	highlight     = "#ffff00"
	bulletText    = "–"
)

// CSP of the page: no network, no scripts, only the inline style sheet and style attributes.
const csp = "default-src 'none'; style-src 'unsafe-inline'"

const (
	pageBreak = `<div class="pb"></div>`
	lineBreak = `<br/>`
	tab       = `<span class="tab"></span>`
)

// page writes the print page. Void elements are self-closed (valid HTML5), so the page is also well-formed XML.
type page struct {
	b         strings.Builder
	newPages  bool // level-1 headings start new pages (Document.SectionsOnNewPage)
	pageStart bool // nothing is written on the current page yet: a level-1 heading needs no page break
	tables    int  // tables written, numbers their captions
	figures   int  // figure captions written
}

// printHTML returns the print page of doc: a complete HTML5 page in UTF-8 with the layout of adapters/docx (A4, the
// profile's margins, font, size, line spacing and first-line indent, justified body, level-1 headings on a new page,
// page numbers bottom centre except on the title page). A document that fails values.Document.Validate is refused;
// an invalid formatting field falls back to its GOST default. The same document gives the same bytes.
func printHTML(doc values.Document) ([]byte, error) {
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	f := doc.Format.Normalized()
	title := !doc.Title.IsZero()

	p := &page{newPages: doc.SectionsOnNewPage, pageStart: true}
	p.head(doc.Title, f, title)
	if title {
		p.titlePage(doc.Title, doc.TableOfContents || len(doc.Blocks) > 0)
	}
	if doc.TableOfContents {
		p.contents(doc.Blocks, len(doc.Blocks) > 0)
	}
	for i, bl := range doc.Blocks {
		if err := p.block(bl); err != nil {
			return nil, fmt.Errorf("%w: block %d: %w", ErrRender, i, err)
		}
	}
	p.b.WriteString(`</body></html>`)
	return []byte(p.b.String()), nil
}

// head writes the page up to <body>: charset, CSP, title and the style sheet. The font name is not escaped for CSS:
// Normalized keeps only names that pass values.ValidFontName (letters, digits, space, '-', '_', '.').
func (p *page) head(t values.TitlePage, f values.Formatting, title bool) {
	b := &p.b
	b.WriteString(`<!DOCTYPE html>` + "\n" + `<html lang="ru"><head><meta charset="utf-8"/>` +
		`<meta http-equiv="Content-Security-Policy" content="` + csp + `"/><title>`)
	b.WriteString(escape(t.Heading))
	b.WriteString(`</title><style>`)

	font := `"` + f.FontName + `"`
	size := strconv.Itoa(f.FontSizePt) + "pt"
	fmt.Fprintf(b, `@page{size:A4;margin:%dmm %dmm %dmm %dmm;`+
		`@bottom-center{content:counter(page);font-family:%s;font-size:%s}}`,
		f.MarginTopMM, f.MarginRightMM, f.MarginBottomMM, f.MarginLeftMM, font, size)
	if title {
		b.WriteString(`@page:first{@bottom-center{content:none}}`)
	}
	fmt.Fprintf(b, `html{print-color-adjust:exact;-webkit-print-color-adjust:exact}`+
		`body{margin:0;font-family:%s;font-size:%s;line-height:%s}`+
		`p,h1,h2,h3{margin:0;font-size:%s;font-weight:normal;text-indent:%s;text-align:justify}`+
		`h1,h2,h3{font-weight:bold;text-align:left;margin:%s 0 %s;break-after:avoid}h3{font-style:italic}`,
		font, size, ratio(f.LineSpacingPct), size, mm(f.IndentTenthMM), pt(headingBefore), pt(headingAfter))
	fmt.Fprintf(b, `.ni{text-indent:0}.c{text-align:center}.r{text-align:right}.l{text-align:left}.b{font-weight:bold}`+
		`.big{font-size:%dpt}.g1{margin-top:%s}.g2{margin-top:%s}.g3{margin-top:%s}.g4{margin-top:%s}`,
		titleSizePt, pt(headingGap), pt(topicGap), pt(signGap), pt(placeGap))
	for level := values.MinHeadingLevel; level <= values.MaxHeadingLevel; level++ {
		fmt.Fprintf(b, `.toc%d{text-indent:0;text-align:left;margin-left:%s}`, level, pt((level-1)*tocIndent))
	}
	fmt.Fprintf(b, `.keep{break-after:avoid}.hl{background-color:%s}.pb{break-after:page}.np{break-before:page}`+
		`.tab{display:inline-block;width:%s}`+
		`table{width:100%%;table-layout:fixed;border-collapse:collapse}thead{display:table-header-group}`+
		`td,th{border:%gpt solid #000;padding:0 %s;vertical-align:top;text-align:left;line-height:1;font-weight:normal}`+
		`th{font-weight:bold}`,
		highlight, pt(tabStop), borderPt, pt(cellPadding))
	b.WriteString(`</style></head><body>`)
}

// titlePage writes the lines of the title page that are set, then a page break when more follows (no blank page).
func (p *page) titlePage(t values.TitlePage, more bool) {
	lines := []struct {
		text, class string
	}{
		{t.University, "ni c"},
		{t.Faculty, "ni c"},
		{t.Heading, "ni c b big g1"},
		{t.Topic, "ni c g2"},
		{t.Performer, "ni r g3"},
		{t.Checker, "ni r"},
		{t.Place, "ni c g4"},
	}
	for _, l := range lines {
		if l.text == "" {
			continue
		}
		p.paragraph("p", l.class, l.text)
	}
	if more {
		p.breakPage()
	}
}

func (p *page) breakPage() {
	p.b.WriteString(pageBreak)
	p.pageStart = true
}

// contents writes the TOC heading and the headings of the blocks, the bibliography heading included. The .docx gets
// its page numbers from Word; a print page cannot compute them, so the entries have none.
func (p *page) contents(blocks []values.Block, more bool) {
	p.paragraph("p", "ni c b", values.ContentsHeading)
	n := 0
	for _, bl := range blocks {
		switch bl.Kind {
		case values.BlockHeading:
			p.paragraph("p", "toc"+strconv.Itoa(headingLevel(bl.Level)), strings.Join(bl.Lines, " "))
			n++
		case values.BlockBibliography:
			p.paragraph("p", "toc"+strconv.Itoa(values.MinHeadingLevel), values.BibliographyHeading)
			n++
		}
	}
	if n == 0 {
		p.paragraph("p", "toc1", "") // the empty TOC field of the .docx is an empty line
	}
	p.pageStart = false
	if more {
		p.breakPage()
	}
}

func headingLevel(level int) int {
	return min(max(level, values.MinHeadingLevel), values.MaxHeadingLevel)
}

// block writes one block of the body; a kind it does not know is an error, never skipped.
func (p *page) block(bl values.Block) error {
	switch bl.Kind {
	case values.BlockHeading:
		p.heading(headingLevel(bl.Level), strings.Join(bl.Lines, " "), "")
	case values.BlockParagraph:
		p.lines(bl.Lines)
	case values.BlockBulletList:
		p.items(bl.Lines, func(int) string { return bulletText })
	case values.BlockNumberedList:
		p.items(bl.Lines, func(n int) string { return strconv.Itoa(n) + ")" })
	case values.BlockTable:
		p.table(bl.Table, bl.Caption)
	case values.BlockCaption:
		p.paragraph("p", "ni c", strings.Join(bl.Lines, " "))
	case values.BlockBibliography:
		p.heading(values.MinHeadingLevel, values.BibliographyHeading, "ni c")
		p.items(bl.Lines, func(n int) string { return strconv.Itoa(n) + "." })
	case values.BlockPlaceholder:
		p.placeholder(strings.Join(bl.Lines, " "), bl.Caption)
	default:
		return fmt.Errorf("unknown block kind %q", bl.Kind)
	}
	p.pageStart = false
	return nil
}

// heading writes a heading; with newPages, a level-1 heading starts a new page unless the page is still empty.
func (p *page) heading(level int, s, class string) {
	if p.newPages && level == values.MinHeadingLevel && !p.pageStart {
		class = strings.TrimSpace(class + " np")
	}
	p.paragraph("h"+strconv.Itoa(level), class, s)
}

// lines writes a body paragraph whose lines are separated by line breaks.
func (p *page) lines(lines []string) {
	b := &p.b
	b.WriteString(`<p>`)
	for i, line := range lines {
		if i > 0 {
			b.WriteString(lineBreak)
		}
		text(b, line)
	}
	// A block ending in a line break shows no empty line: one more break keeps the empty last line (or paragraph).
	if len(lines) == 0 || clean(lines[len(lines)-1]) == "" {
		b.WriteString(lineBreak)
	}
	b.WriteString(`</p>`)
}

// items writes list items as the .docx numbering does: the marker at the paragraph indent, a space, the text wrapping
// to the margin.
func (p *page) items(lines []string, marker func(n int) string) {
	for i, line := range lines {
		p.b.WriteString(`<p>` + marker(i+1) + " ")
		text(&p.b, line)
		p.b.WriteString(`</p>`)
	}
}

// table writes the numbered caption above the table, kept with it, then the table: equal columns, the header row
// bold and repeated on every page, rows padded to the column count.
func (p *page) table(t values.Table, name string) {
	p.tables++
	p.paragraph("p", "ni l keep", values.TableCaption(p.tables, name))
	cols := t.Columns()
	b := &p.b
	b.WriteString(`<table>`)
	row := func(cells []string, cell string) {
		b.WriteString(`<tr>`)
		for i := range cols {
			s := ""
			if i < len(cells) {
				s = cells[i]
			}
			p.paragraph(cell, "", s)
		}
		b.WriteString(`</tr>`)
	}
	if len(t.Header) > 0 {
		b.WriteString(`<thead>`)
		row(t.Header, "th")
		b.WriteString(`</thead>`)
	}
	b.WriteString(`<tbody>`)
	for _, r := range t.Rows {
		row(r, "td")
	}
	b.WriteString(`</tbody></table>`)
}

// placeholder writes the highlighted prompt the student replaces; with a name, the numbered figure caption below,
// kept on the page of the placeholder.
func (p *page) placeholder(prompt, figure string) {
	class := "ni c"
	if figure != "" {
		class += " keep"
	}
	p.b.WriteString(`<p class="` + class + `"><span class="hl">`)
	text(&p.b, values.PlaceholderText(prompt))
	p.b.WriteString(`</span></p>`)
	if figure != "" {
		p.figures++
		p.paragraph("p", "ni c", values.FigureCaption(p.figures, figure))
	}
}

// paragraph writes <tag class="class">s</tag>; an empty text keeps its line, as an empty Word paragraph does.
func (p *page) paragraph(tag, class, s string) {
	b := &p.b
	b.WriteString(`<` + tag)
	if class != "" {
		b.WriteString(` class="` + class + `"`)
	}
	b.WriteString(`>`)
	if clean(s) == "" {
		b.WriteString(lineBreak)
	} else {
		text(b, s)
	}
	b.WriteString(`</` + tag + `>`)
}

// text writes s escaped, as adapters/docx writes a run: tabs become tab spans, line breaks become spaces.
func text(b *strings.Builder, s string) {
	for i, part := range strings.Split(clean(s), "\t") {
		if i > 0 {
			b.WriteString(tab)
		}
		b.WriteString(escape(part))
	}
}

// escape cleans s and escapes it as html.EscapeString does, plus ':' and '=': they render the same, and no text can
// then spell a URL scheme or an attribute (the page holds no "http:" or "src=", even as text).
func escape(s string) string {
	var b strings.Builder
	for _, r := range clean(s) {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&#34;")
		case '\'':
			b.WriteString("&#39;")
		case ':':
			b.WriteString("&#58;")
		case '=':
			b.WriteString("&#61;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// clean drops what adapters/docx drops (invalid UTF-8, characters XML cannot hold, the bidi controls, which could
// disguise model text) and the C1 controls, which HTML treats as errors; line breaks become spaces.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return r
		case r == '\n' || r == '\r':
			return ' '
		case r >= 0x20 && r < 0x7F, r >= 0xA0 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
			return r
		}
		return -1
	}, strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\r\n", " "))
	return values.StripBidi(s)
}

// pt formats twips as points.
func pt(twips int) string { return strconv.FormatFloat(float64(twips)/20, 'f', -1, 64) + "pt" }

// mm formats tenths of a millimetre as millimetres.
func mm(tenths int) string { return strconv.FormatFloat(float64(tenths)/10, 'f', -1, 64) + "mm" }

// ratio formats a line spacing in percent as a CSS line-height.
func ratio(pct int) string { return strconv.FormatFloat(float64(pct)/100, 'f', -1, 64) }
