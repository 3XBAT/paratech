package docx

import (
	"fmt"
	"strconv"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

// Layout of the title page and of the body; lengths in twips, sizes in half points.
const (
	pageBreak = `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`
	noIndent  = `<w:ind w:firstLine="0"/>`

	headingGap = 3600 // before the heading of the title page
	topicGap   = 120
	signGap    = 2400 // before «Выполнил»
	placeGap   = 2400 // before the city and the year
	titleSize  = 32
	center     = "center"
	right      = "right"
	left       = "left"

	tocField     = ` TOC \o "1-3" \h \z \u `
	bookmarkName = "_Toc" // + the number of the heading, as Word names TOC targets
)

// body writes word/document.xml. It is a single pass; the numbered lists it meets are recorded for numbering.xml.
type body struct {
	b         strings.Builder
	f         values.Formatting
	toc       bool
	newPages  bool  // level-1 headings start new pages (Document.SectionsOnNewPage)
	pageStart bool  // nothing is written on the current page yet: a level-1 heading needs no page break
	headings  int   // headings written, numbers their bookmarks
	tables    int   // tables written, numbers their captions
	figures   int   // figure captions written
	lists     []int // the definition (abstractNumId) of every numbered list instance
}

// heading is a heading of the document as the TOC lists it.
type heading struct {
	level int
	text  string
}

// document renders word/document.xml and returns the definitions of its numbered lists in instance order.
func document(doc values.Document, f values.Formatting) (string, []int) {
	w := &body{f: f, toc: doc.TableOfContents, newPages: doc.SectionsOnNewPage, pageStart: true}
	w.b.WriteString(xmlHeader + `<w:document xmlns:w="` + wordNS + `" xmlns:r="` + relNS + `"><w:body>`)
	title := !doc.Title.IsZero()
	if title {
		w.titlePage(doc.Title, doc.TableOfContents || len(doc.Blocks) > 0)
	}
	if doc.TableOfContents {
		w.contents(headings(doc.Blocks), len(doc.Blocks) > 0)
	}
	for i, bl := range doc.Blocks {
		w.block(bl)
		// A body must not end with a table: Word wants a paragraph before the section properties.
		if i == len(doc.Blocks)-1 && bl.Kind == values.BlockTable {
			w.b.WriteString(`<w:p/>`)
		}
	}
	w.sectPr(title)
	w.b.WriteString(`</w:body></w:document>`)
	return w.b.String(), w.lists
}

// sectPr: A4 with the profile's margins, the page number footer; with a title page, titlePg gives the first page the
// empty footer.
func (w *body) sectPr(title bool) {
	f := w.f
	top, bottom := twipsFromMM(f.MarginTopMM), twipsFromMM(f.MarginBottomMM)
	w.b.WriteString(`<w:sectPr><w:footerReference w:type="default" r:id="` + footerRel + `"/>`)
	if title {
		w.b.WriteString(`<w:footerReference w:type="first" r:id="` + firstFooterRel + `"/>`)
	}
	fmt.Fprintf(&w.b, `<w:pgSz w:w="%d" w:h="%d"/>`+
		`<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="0"/>`,
		pageWidth, pageHeight, top, twipsFromMM(f.MarginRightMM), bottom, twipsFromMM(f.MarginLeftMM),
		min(headerFooter, top*2/3), min(headerFooter, bottom*2/3)) // a small margin keeps the footer inside it
	if title {
		w.b.WriteString(`<w:titlePg/>`)
	}
	w.b.WriteString(`</w:sectPr>`)
}

// titlePage writes the lines of the title page that are set, then a page break when more follows (no blank page).
func (w *body) titlePage(t values.TitlePage, more bool) {
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
	b := &w.b
	for _, l := range lines {
		if l.text == "" {
			continue
		}
		fmt.Fprintf(b, `<w:p><w:pPr><w:spacing w:before="%d"/>`+noIndent+`<w:jc w:val="%s"/></w:pPr><w:r>`, l.before, l.align)
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
	if more {
		w.breakPage()
	}
}

func (w *body) breakPage() {
	w.b.WriteString(pageBreak)
	w.pageStart = true
}

// headings lists the headings of blocks as the TOC shows them, the bibliography heading included.
func headings(blocks []values.Block) []heading {
	var out []heading
	for _, bl := range blocks {
		switch bl.Kind {
		case values.BlockHeading:
			out = append(out, heading{headingLevel(bl.Level), strings.Join(bl.Lines, " ")})
		case values.BlockBibliography:
			out = append(out, heading{values.MinHeadingLevel, values.BibliographyHeading})
		}
	}
	return out
}

func headingLevel(level int) int {
	return min(max(level, values.MinHeadingLevel), values.MaxHeadingLevel)
}

// contents writes the TOC heading and the TOC field. Its cached result lists the headings without page numbers:
// Word replaces it on open (settings updateFields), LibreOffice shows it as is.
func (w *body) contents(hs []heading, more bool) {
	b := &w.b
	b.WriteString(`<w:p><w:pPr>` + noIndent + `<w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:b/></w:rPr>`)
	text(b, values.ContentsHeading)
	b.WriteString(`</w:r></w:p>`)
	begin := `<w:r><w:fldChar w:fldCharType="begin" w:dirty="true"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve">` + tocField + `</w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`
	end := `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
	if len(hs) == 0 {
		b.WriteString(`<w:p>` + begin + end + `</w:p>`)
	}
	for i, h := range hs {
		fmt.Fprintf(b, `<w:p><w:pPr><w:pStyle w:val="%s%d"/></w:pPr>`, styleTOC, h.level)
		if i == 0 {
			b.WriteString(begin)
		}
		b.WriteString(`<w:hyperlink w:anchor="` + bookmark(i+1) + `" w:history="1"><w:r>`)
		text(b, h.text)
		b.WriteString(`</w:r></w:hyperlink></w:p>`)
	}
	if len(hs) > 0 {
		b.WriteString(`<w:p>` + end + `</w:p>`)
	}
	w.pageStart = false
	if more {
		w.breakPage()
	}
}

func bookmark(n int) string { return bookmarkName + strconv.Itoa(n) }

// block writes one block of the body.
func (w *body) block(bl values.Block) {
	switch bl.Kind {
	case values.BlockHeading:
		w.heading(headingLevel(bl.Level), strings.Join(bl.Lines, " "), "")
	case values.BlockBulletList:
		w.items(bl.Lines, bulletNum)
	case values.BlockNumberedList:
		w.items(bl.Lines, w.newList(abstractDecimal))
	case values.BlockTable:
		w.table(bl.Table, bl.Caption)
	case values.BlockCaption:
		w.caption(strings.Join(bl.Lines, " "), center, false)
	case values.BlockBibliography:
		w.heading(values.MinHeadingLevel, values.BibliographyHeading, noIndent+`<w:jc w:val="center"/>`)
		w.items(bl.Lines, w.newList(abstractBibliography))
	case values.BlockPlaceholder:
		w.placeholder(strings.Join(bl.Lines, " "), bl.Caption)
	default:
		w.paragraph(bl.Lines)
	}
	w.pageStart = false
}

// heading writes a heading paragraph; with newPages, a level-1 heading starts a new page unless the page is still
// empty (GOST 7.32). With a TOC, the heading is the target of its entry.
func (w *body) heading(level int, s, pPr string) {
	b := &w.b
	fmt.Fprintf(b, `<w:p><w:pPr><w:pStyle w:val="%s%d"/>`, styleHeading, level)
	if w.newPages && level == values.MinHeadingLevel && !w.pageStart {
		b.WriteString(`<w:pageBreakBefore/>`)
	}
	b.WriteString(pPr + `</w:pPr>`)
	w.headings++
	if w.toc {
		fmt.Fprintf(b, `<w:bookmarkStart w:id="%d" w:name="%s"/>`, w.headings, bookmark(w.headings))
	}
	b.WriteString(`<w:r>`)
	text(b, s)
	b.WriteString(`</w:r>`)
	if w.toc {
		fmt.Fprintf(b, `<w:bookmarkEnd w:id="%d"/>`, w.headings)
	}
	b.WriteString(`</w:p>`)
}

// paragraph writes a body paragraph (the Normal style) whose lines are separated by line breaks.
func (w *body) paragraph(lines []string) {
	b := &w.b
	b.WriteString(`<w:p><w:r>`)
	for i, line := range lines {
		if i > 0 {
			b.WriteString(`<w:br/>`)
		}
		text(b, line)
	}
	b.WriteString(`</w:r></w:p>`)
}

// newList starts a numbered list instance of definition abstract and returns its numId.
func (w *body) newList(abstract int) int {
	w.lists = append(w.lists, abstract)
	return firstListNum + len(w.lists) - 1
}

func (w *body) items(lines []string, num int) {
	b := &w.b
	for _, line := range lines {
		fmt.Fprintf(b, `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="%d"/></w:numPr></w:pPr><w:r>`, num)
		text(b, line)
		b.WriteString(`</w:r></w:p>`)
	}
}

// caption writes a caption paragraph aligned as given; a caption above a table stays on its page (keepNext).
func (w *body) caption(s, align string, keepNext bool) {
	b := &w.b
	fmt.Fprintf(b, `<w:p><w:pPr><w:pStyle w:val="%s"/>`, styleCaption)
	if keepNext {
		b.WriteString(`<w:keepNext/>`)
	}
	fmt.Fprintf(b, `<w:jc w:val="%s"/></w:pPr><w:r>`, align)
	text(b, s)
	b.WriteString(`</w:r></w:p>`)
}

// table writes the numbered caption above the table (GOST 7.32: «Таблица N – …», left), then the table; the header
// row repeats on every page and is bold. Columns share the text width; rows are padded to the column count.
func (w *body) table(t values.Table, name string) {
	w.tables++
	w.caption(values.TableCaption(w.tables, name), left, true)
	cols := t.Columns()
	width := textWidth(w.f)
	colWidth := func(i int) int {
		if i == cols-1 {
			return width - (cols-1)*(width/cols)
		}
		return width / cols
	}
	b := &w.b
	b.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="` + styleTable + `"/><w:tblW w:w="5000" w:type="pct"/>` +
		tableBorders + `<w:tblLayout w:type="fixed"/></w:tblPr><w:tblGrid>`)
	for i := range cols {
		fmt.Fprintf(b, `<w:gridCol w:w="%d"/>`, colWidth(i))
	}
	b.WriteString(`</w:tblGrid>`)
	row := func(cells []string, header bool) {
		b.WriteString(`<w:tr>`)
		if header {
			b.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for i := range cols {
			fmt.Fprintf(b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr><w:p><w:pPr><w:pStyle w:val="%s"/></w:pPr>`,
				colWidth(i), styleCell)
			if i < len(cells) && cells[i] != "" {
				b.WriteString(`<w:r>`)
				if header {
					b.WriteString(`<w:rPr><w:b/></w:rPr>`)
				}
				text(b, cells[i])
				b.WriteString(`</w:r>`)
			}
			b.WriteString(`</w:p></w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	if len(t.Header) > 0 {
		row(t.Header, true)
	}
	for _, r := range t.Rows {
		row(r, false)
	}
	b.WriteString(`</w:tbl>`)
}

// placeholder writes the highlighted prompt the student replaces; with a name, the numbered figure caption below.
func (w *body) placeholder(prompt, figure string) {
	b := &w.b
	b.WriteString(`<w:p><w:pPr>`)
	if figure != "" {
		b.WriteString(`<w:keepNext/>`)
	}
	b.WriteString(noIndent + `<w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:highlight w:val="yellow"/></w:rPr>`)
	text(b, values.PlaceholderText(prompt))
	b.WriteString(`</w:r></w:p>`)
	if figure != "" {
		w.figures++
		w.caption(values.FigureCaption(w.figures, figure), center, false)
	}
}
