// Package docx renders a student paper (values.Document) as a Word file (TASK-094, TASK-104): an Office Open XML
// package of the content types, the relationships, word/document.xml, styles, settings, numbering and the page
// footers, zipped with archive/zip. Standard library only; Word and LibreOffice open it. The output is
// deterministic: the same document gives the same bytes (an email attachment is pinned by its digest).
//
// Every text is data: characters that XML cannot hold are dropped and the rest is escaped, so a text cannot add
// markup. The layout follows GOST 7.32 with the document's formatting profile (values.Formatting): A4, the profile's
// margins, font, size and line spacing, justified body with a first-line indent, level-1 headings on a new page, page
// numbers bottom centre except on the title page, a TOC field Word refreshes on open, tables with a numbered caption
// above.
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

// ErrRender means the package could not be assembled, or the document is beyond the limits (values.ErrInvalidDocument).
var ErrRender = errors.New("docx: the document cannot be rendered")

// Renderer renders documents. The zero value is ready to use.
type Renderer struct{}

// New returns a Renderer.
func New() Renderer { return Renderer{} }

// Render returns the .docx bytes of doc. A document that fails values.Document.Validate is refused; an invalid
// formatting field falls back to its GOST default.
func (Renderer) Render(ctx context.Context, doc values.Document) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	f := doc.Format.Normalized()
	body, lists := document(doc, f)
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"word/document.xml", body},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", styles(f)},
		{"word/settings.xml", settings(doc.TableOfContents)},
		{"word/numbering.xml", numbering(f, lists)},
		{"word/" + footerPart, footer},
		{"word/" + firstFooterPart, firstFooter},
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

// text writes s as the text of a run: tabs become tab elements, line breaks become spaces, and the characters XML
// cannot hold and the bidi controls (they could disguise model text) are dropped before the rest is escaped.
func text(b *strings.Builder, s string) {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(values.StripBidi(xmlChars(s)))
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

// attr returns s escaped for an attribute value (EscapeText escapes quotes and line breaks too).
func attr(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(xmlChars(s)))
	return b.String()
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
