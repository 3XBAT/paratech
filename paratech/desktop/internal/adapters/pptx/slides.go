package pptx

import (
	"encoding/xml"
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

// Font sizes in hundredths of a point; six bullets of two lines fit the body.
const (
	coverTitleSize   = 4000
	subtitleSize     = 2000
	titleSize        = 3600
	bulletSize       = 2000
	headingSize      = 2200
	columnBulletSize = 1800
	conclusionSize   = 2400

	bulletIndent = 342900 // EMU
	bulletChar   = "•"
)

// font is the look of a run; zero fields are inherited.
type font struct {
	size  int
	bold  bool
	color string
	major bool // the heading font of the theme
}

// slide renders a slide and returns it with the number of its layout.
func slide(s values.Slide, pal palette) (string, int) {
	var b strings.Builder
	b.WriteString(xmlHeader + `<p:sld ` + pmlNS + `><p:cSld>` + spTreeStart)
	id := 1
	next := func() int { id++; return id }

	title := font{size: titleSize, bold: true, color: pal.text, major: true}
	layout := layoutContent
	switch s.Kind {
	case values.SlideTitle:
		layout = layoutCover
		title.size = coverTitleSize
		shape(&b, next(), titleCover, plain(oneLine(s.Title), title, 0))
		accentBar(&b, next(), coverAccent, pal.accent)
		if lines := textLines(s.Subtitle, true); len(lines) > 0 {
			var p strings.Builder
			for _, l := range lines {
				p.WriteString(plain(l, font{size: subtitleSize, color: pal.muted}, 600))
			}
			shape(&b, next(), subtitleCov, p.String())
		}
	case values.SlideTwoColumns:
		layout = layoutColumns
		shape(&b, next(), titlePage, plain(oneLine(s.Title), title, 0))
		accentBar(&b, next(), pageAccent, pal.accent)
		shape(&b, next(), bodyLeft, column(s.Left, pal))
		shape(&b, next(), bodyRight, column(s.Right, pal))
	case values.SlideConclusion:
		shape(&b, next(), titlePage, plain(oneLine(s.Title), title, 0))
		accentBar(&b, next(), pageAccent, pal.accent)
		if len(s.Bullets) > 0 {
			var p strings.Builder
			for _, l := range s.Bullets {
				p.WriteString(plain(oneLine(l), font{size: conclusionSize, color: pal.text}, 1200))
			}
			shape(&b, next(), bodyPage, p.String())
		}
	default: // values.SlideBullets; Validate admits no other kind
		shape(&b, next(), titlePage, plain(oneLine(s.Title), title, 0))
		accentBar(&b, next(), pageAccent, pal.accent)
		var p strings.Builder
		for _, l := range s.Bullets {
			p.WriteString(bullet(oneLine(l), font{size: bulletSize, color: pal.text}, pal.muted))
		}
		shape(&b, next(), bodyPage, p.String())
	}

	b.WriteString(`</p:spTree></p:cSld>` + masterMapping + `</p:sld>`)
	return b.String(), layout
}

// column renders the paragraphs of a column: its heading, if any, then its bullets.
func column(c values.SlideColumn, pal palette) string {
	var b strings.Builder
	if h := oneLine(c.Heading); h != "" {
		b.WriteString(plain(h, font{size: headingSize, bold: true, color: pal.text}, 0))
	}
	for _, l := range c.Bullets {
		b.WriteString(bullet(oneLine(l), font{size: columnBulletSize, color: pal.text}, pal.muted))
	}
	return b.String()
}

// notesSlide renders the speaker notes of a slide, one paragraph per line.
func notesSlide(notes string) string {
	var p strings.Builder
	for _, l := range textLines(notes, false) {
		p.WriteString(`<a:p>` + run(l, font{}) + `</a:p>`)
	}
	if p.Len() == 0 {
		p.WriteString(emptyParagraph)
	}
	var b strings.Builder
	b.WriteString(xmlHeader + `<p:notes ` + pmlNS + `><p:cSld>` + spTreeStart)
	notesShapes(&b, p.String())
	b.WriteString(`</p:spTree></p:cSld>` + masterMapping + `</p:notes>`)
	return b.String()
}

// plain is a paragraph without a bullet, with spaceBefore in hundredths of a point.
func plain(s string, f font, spaceBefore int) string {
	return fmt.Sprintf(`<a:p><a:pPr marL="0" indent="0"><a:spcBef><a:spcPts val="%d"/></a:spcBef><a:buNone/></a:pPr>%s</a:p>`,
		spaceBefore, run(s, f))
}

func bullet(s string, f font, color string) string {
	return fmt.Sprintf(`<a:p><a:pPr marL="%d" indent="-%d"><a:spcBef><a:spcPts val="1000"/></a:spcBef>`+
		`<a:buClr><a:srgbClr val="%s"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="%s"/></a:pPr>%s</a:p>`,
		bulletIndent, bulletIndent, color, bulletChar, run(s, f))
}

// run renders s as a text run, or as the end of an empty paragraph.
func run(s string, f font) string {
	var b strings.Builder
	attrs := `lang="ru-RU"`
	if f.size != 0 {
		attrs += fmt.Sprintf(` sz="%d"`, f.size)
	}
	if f.bold {
		attrs += ` b="1"`
	}
	props := ""
	if f.color != "" {
		props += `<a:solidFill><a:srgbClr val="` + f.color + `"/></a:solidFill>`
	}
	if f.major {
		props += majorFonts
	} else if f.size != 0 {
		props += minorFonts
	}

	if s == "" {
		b.WriteString(`<a:endParaRPr ` + attrs + ` dirty="0">` + props + `</a:endParaRPr>`)
		return b.String()
	}
	b.WriteString(`<a:r><a:rPr ` + attrs + ` dirty="0">` + props + `</a:rPr><a:t>`)
	escape(&b, s)
	b.WriteString(`</a:t></a:r>`)
	return b.String()
}

// oneLine is s cleaned, with every run of white space (line breaks and tabs too) turned into one space.
func oneLine(s string) string { return strings.Join(strings.Fields(clean(s)), " ") }

// textLines splits s into cleaned lines without trailing spaces; tabs become spaces. Blank lines are dropped when
// skipBlank is set, otherwise only the leading and trailing ones.
func textLines(s string, skipBlank bool) []string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(clean(s))
	s = strings.Trim(s, "\n")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, " ")
		if l == "" && skipBlank {
			continue
		}
		out = append(out, l)
	}
	return out
}

// clean drops the characters XML cannot hold and the bidi controls, which can make text look different from what it
// says.
func clean(s string) string { return values.StripBidi(xmlChars(s)) }

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

// escape writes s escaped as XML character data.
func escape(b *strings.Builder, s string) {
	_ = xml.EscapeText(b, []byte(s)) // a strings.Builder never fails a write
}
