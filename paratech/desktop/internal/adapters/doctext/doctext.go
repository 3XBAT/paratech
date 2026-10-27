package doctext

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"paratech/desktop/internal/domain/library/material"
)

// Operation names of the errors.
const (
	opExtract = "doctext: extract"
)

// Limits of the reader. Unpacked sizes are counted while reading, never taken from the ZIP headers.
const (
	maxEntries    = 10_000
	maxPartBytes  = 32 << 20  // one XML part unpacked
	maxTotalBytes = 128 << 20 // all parts read of one file unpacked
	maxTXTBytes   = 3 * material.MaxTextBytes
)

// Parts of an Office Open XML package.
const (
	docxBody     = "word/document.xml"
	docxStyles   = "word/styles.xml"
	pptxMain     = "ppt/presentation.xml"
	pptxMainRels = "ppt/_rels/presentation.xml.rels"
	pptxDir      = "ppt/"
)

// errTooLarge stops a read at a limit; it is reported as material.ErrTextUnreadable.
var errTooLarge = errors.New("beyond the reader's limits")

// Extractor reads the text of DOCX, PPTX and TXT files. The zero value is ready; it is safe for concurrent use.
type Extractor struct{}

// New returns an Extractor.
func New() *Extractor { return &Extractor{} }

// Extract reads the text of data of type typ. A PDF (and any other type) is material.ErrTextNotExtracted; a damaged
// file, one that is not of its type or one beyond the limits is material.ErrTextUnreadable. A text cut at
// material.MaxTextBytes is returned marked truncated.
//
// The text read is cached per file (ADR-0027 «Text»): a change of what any reader here gives for the same bytes must
// bump material.TextCacheVersion, or old cached texts stay in use.
func (e *Extractor) Extract(ctx context.Context, typ material.Type, data []byte) (material.Text, error) {
	if err := ctx.Err(); err != nil {
		return material.Text{}, fmt.Errorf("%s: %w", opExtract, err)
	}
	var (
		fs        []material.Fragment
		truncated bool
		err       error
	)
	switch typ {
	case material.TypeTXT:
		fs, truncated, err = txt(data)
	case material.TypeDOCX:
		fs, truncated, err = docx(ctx, data)
	case material.TypePPTX:
		fs, truncated, err = pptx(ctx, data)
	default:
		return material.Text{}, fmt.Errorf("%s: %s: %w", opExtract, typ, material.ErrTextNotExtracted)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return material.Text{}, fmt.Errorf("%s: %w", opExtract, ctxErr)
		}
		return material.Text{}, fmt.Errorf("%s: %s: %w: %w", opExtract, typ, material.ErrTextUnreadable, err)
	}
	text, err := material.NewText(fs, truncated)
	if err != nil {
		return material.Text{}, fmt.Errorf("%s: %s: %w: %w", opExtract, typ, material.ErrTextUnreadable, err)
	}
	return text, nil
}

// --- TXT ----------------------------------------------------------------------------------------------------

// txt splits the decoded text into lines; form feeds are page breaks. A file above maxTXTBytes is read up to it.
func txt(data []byte) ([]material.Fragment, bool, error) {
	truncated := false
	if len(data) > maxTXTBytes {
		data, truncated = cutText(data, maxTXTBytes), true
	}
	s, err := material.DecodeText(data)
	if err != nil {
		return nil, false, err
	}
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	if !strings.Contains(s, "\f") {
		// A text file may be Markdown (detected as TXT by content); the hints of its front matter are for the import
		// of notes, not part of the text.
		return material.ReadMarkdown(s).Fragments, truncated, nil
	}
	// Form feeds mark a paged text (a printout, not a note): its pages are the locators, so their text is kept as it is,
	// without the Markdown reading that would cut it into sections instead.
	var fs []material.Fragment
	for i, page := range strings.Split(s, "\f") {
		fs = append(fs, material.Fragment{Locator: material.Locator{Kind: material.LocatorPage, N: i + 1}, Lines: strings.Split(page, "\n")})
	}
	return fs, truncated, nil
}

// cutText cuts a text file to at most n bytes without splitting a character of its encoding.
func cutText(data []byte, n int) []byte {
	cut := data[:n]
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		cut = cut[:len(cut)&^1]
		// A high surrogate at the end has lost its pair.
		last := cut[len(cut)-2:]
		hi := last[1]
		if data[0] == 0xfe {
			hi = last[0]
		}
		if hi >= 0xd8 && hi <= 0xdb {
			cut = cut[:len(cut)-2]
		}
		return cut
	}
	// UTF-8: drop a character the cut split (at most 3 continuation bytes and its start).
	for i := 0; i < 4 && len(cut) > 0; i++ {
		c := cut[len(cut)-1-i]
		if c < 0x80 {
			break
		}
		if c >= 0xc0 { // a start byte: keep its character only if it is complete
			need := 2
			switch {
			case c >= 0xf0:
				need = 4
			case c >= 0xe0:
				need = 3
			}
			if i+1 < need {
				cut = cut[:len(cut)-1-i]
			}
			break
		}
	}
	return cut
}

// --- ZIP parts ----------------------------------------------------------------------------------------------

// pkg is an opened Office Open XML package: its entries by name and the unpacked bytes read so far.
type pkg struct {
	entries map[string]*zip.File
	read    int64
}

func open(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) { // names are never used as paths
		return nil, err
	}
	if len(zr.File) > maxEntries {
		return nil, fmt.Errorf("%w: %d entries", errTooLarge, len(zr.File))
	}
	p := &pkg{entries: make(map[string]*zip.File, len(zr.File))}
	for _, f := range zr.File {
		if _, dup := p.entries[f.Name]; dup {
			return nil, fmt.Errorf("an entry is in the package twice")
		}
		p.entries[f.Name] = f
	}
	return p, nil
}

// part reads one entry, unpacked, within maxPartBytes and what is left of maxTotalBytes.
func (p *pkg) part(name string) ([]byte, error) {
	f, ok := p.entries[name]
	if !ok {
		return nil, fmt.Errorf("a required part is missing")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	limit := min(int64(maxPartBytes), maxTotalBytes-p.read)
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: a part unpacks to more than %d bytes", errTooLarge, limit)
	}
	p.read += int64(len(data))
	return data, nil
}

// --- XML ----------------------------------------------------------------------------------------------------

// Namespaces whose content is skipped: markup compatibility fallbacks repeat the content of their choice.
const nsMarkupCompat = "http://schemas.openxmlformats.org/markup-compatibility/2006"

// textWriter collects the paragraphs of a part, within material.MaxTextBytes in all.
type textWriter struct {
	pages     [][]string // the paragraphs of each page; one page without page breaks
	line      strings.Builder
	size      int
	breaks    bool // a page break was seen
	truncated bool
	heading   bool  // the current paragraph has a heading style (DOCX)
	heads     []int // the indexes in lines of the heading paragraphs
	n         int   // the paragraphs kept so far
}

func newTextWriter() *textWriter { return &textWriter{pages: [][]string{nil}} }

func (w *textWriter) write(s string) {
	if w.truncated {
		return
	}
	if w.size+len(s) > material.MaxTextBytes {
		w.truncated = true
		return
	}
	w.size += len(s)
	w.line.WriteString(s)
}

// endParagraph ends the current line; a heading is only the first line of its paragraph.
func (w *textWriter) endParagraph() {
	if w.line.Len() == 0 {
		return
	}
	if w.heading {
		w.heads = append(w.heads, w.n)
		w.heading = false
	}
	last := len(w.pages) - 1
	w.pages[last] = append(w.pages[last], w.line.String())
	w.line.Reset()
	w.size++
	w.n++
}

// pageBreak starts a new page; a break on a page without text yet is ignored (Word records explicit and rendered
// breaks at the same place).
func (w *textWriter) pageBreak() {
	w.endParagraph()
	if len(w.pages[len(w.pages)-1]) == 0 {
		return
	}
	w.breaks = true
	w.pages = append(w.pages, nil)
}

// lines returns every paragraph in order.
func (w *textWriter) lines() []string {
	var out []string
	for _, p := range w.pages {
		out = append(out, p...)
	}
	return out
}

// walk decodes an XML part and calls start and end for its elements and text for character data; elements of the
// markup compatibility fallback are skipped.
func walk(ctx context.Context, data []byte, start func(xml.StartElement, *xml.Decoder) error, end func(xml.EndElement),
	text func(xml.CharData)) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for n := 0; ; n++ {
		if n%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == nsMarkupCompat && t.Name.Local == "Fallback" {
				if err := dec.Skip(); err != nil {
					return err
				}
				continue
			}
			if err := start(t, dec); err != nil {
				return err
			}
		case xml.EndElement:
			end(t)
		case xml.CharData:
			text(t)
		}
	}
}

// --- DOCX ---------------------------------------------------------------------------------------------------

// docx reads the paragraphs of the main document part in order. Paragraphs of the styles Heading 1–3 cut it into
// sections (material.SectionsOf) when there are at least two; else it has pages or fragments.
func docx(ctx context.Context, data []byte) ([]material.Fragment, bool, error) {
	p, err := open(data)
	if err != nil {
		return nil, false, err
	}
	styles := headingStyles(ctx, p)
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	body, err := p.part(docxBody)
	if err != nil {
		return nil, false, err
	}
	w := newTextWriter()
	inText := 0
	err = walk(ctx, body,
		func(s xml.StartElement, dec *xml.Decoder) error {
			switch s.Name.Local {
			case "p":
				w.endParagraph() // a paragraph inside another (a text box) starts a line of its own
				w.heading = false
			case "pStyle":
				w.heading = isHeading(attr(s, "val"), styles)
			case "t":
				inText++
			case "tab", "cr":
				w.write(" ")
			case "br":
				if attr(s, "type") == "page" {
					w.pageBreak()
				} else {
					w.write(" ")
				}
			case "lastRenderedPageBreak":
				w.pageBreak()
			// Deleted text of tracked changes, field codes, the former properties of a tracked format change.
			case "delText", "instrText", "pPrChange":
				return dec.Skip()
			}
			return nil
		},
		func(e xml.EndElement) {
			switch e.Name.Local {
			case "p":
				w.endParagraph()
				w.heading = false
			case "t":
				inText = max(inText-1, 0)
			}
		},
		func(c xml.CharData) {
			if inText > 0 {
				w.write(string(c))
			}
		})
	if err != nil {
		return nil, false, err
	}
	w.endParagraph()
	if fs, ok := material.SectionsOf(w.lines(), w.heads); ok {
		return fs, w.truncated, nil
	}
	return pages(w), w.truncated, nil
}

// headingStyles returns the ids of the paragraph styles named «heading 1» to «heading 3» in the styles part: Word
// keeps these built-in names in English whatever the language, while the id follows the language («1» in a Russian
// Word). The part is optional: a missing, damaged or too large one gives no ids, and the ids «Heading1» to «Heading3»
// still count (isHeading).
func headingStyles(ctx context.Context, p *pkg) map[string]bool {
	ids := map[string]bool{}
	if _, ok := p.entries[docxStyles]; !ok {
		return ids
	}
	data, err := p.part(docxStyles)
	if err != nil {
		return ids
	}
	cur := ""
	err = walk(ctx, data,
		func(s xml.StartElement, _ *xml.Decoder) error {
			switch s.Name.Local {
			case "style":
				cur = ""
				if attr(s, "type") == "paragraph" {
					cur = attr(s, "styleId")
				}
			case "name":
				level, ok := strings.CutPrefix(strings.ToLower(attr(s, "val")), "heading ")
				if cur != "" && ok && headingLevel(level) {
					ids[cur] = true
				}
			}
			return nil
		},
		func(e xml.EndElement) {
			if e.Name.Local == "style" {
				cur = ""
			}
		},
		func(xml.CharData) {})
	if err != nil {
		return map[string]bool{}
	}
	return ids
}

// isHeading reports whether the paragraph style id is a heading of level 1 to 3.
func isHeading(id string, styles map[string]bool) bool {
	if styles[id] {
		return true
	}
	level, ok := strings.CutPrefix(strings.ToLower(id), "heading")
	return ok && headingLevel(level)
}

func headingLevel(s string) bool { return s == "1" || s == "2" || s == "3" }

// pages are the fragments of a document: its pages when it has page breaks, else fragments of bounded size.
func pages(w *textWriter) []material.Fragment {
	if !w.breaks {
		return material.FragmentsOf(w.lines())
	}
	fs := make([]material.Fragment, 0, len(w.pages))
	for i, lines := range w.pages {
		fs = append(fs, material.Fragment{Locator: material.Locator{Kind: material.LocatorPage, N: i + 1}, Lines: lines})
	}
	return fs
}

// attr returns the value of the attribute with local name local, in any namespace.
func attr(s xml.StartElement, local string) string {
	for _, a := range s.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
