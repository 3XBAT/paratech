package docx

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

// Lengths in WordprocessingML: twentieths of a point (twips) for distances, half points for font sizes.
const (
	pageWidth    = 11906 // A4, twips
	pageHeight   = 16838
	singleLine   = 240 // the line value of single spacing with lineRule auto
	headerFooter = 709 // distance of the footer from the page edge, 1.25 cm
	tocIndent    = 283 // per TOC level, 0.5 cm
)

// Style ids; the built-in names in styles() make Word's navigation pane and TOC field recognise them.
const (
	styleHeading = "Heading" // + level
	styleTOC     = "TOC"     // + level
	styleCaption = "Caption"
	styleTable   = "TableGrid"
	styleCell    = "TableText"
)

// twipsFromTenthMM converts tenths of a millimetre to twips, rounded: 1 mm = 1440/25.4 twips.
func twipsFromTenthMM(t int) int { return (t*2880 + 254) / 508 }

func twipsFromMM(mm int) int { return twipsFromTenthMM(mm * 10) }

// lineValue is the w:line of a spacing in percent of a single line (150: 360).
func lineValue(pct int) int { return (singleLine*pct + 50) / 100 }

// textWidth is the width between the margins, twips.
func textWidth(f values.Formatting) int {
	return pageWidth - twipsFromMM(f.MarginLeftMM) - twipsFromMM(f.MarginRightMM)
}

// styles: the defaults from the profile (font, size, Russian, line spacing), Normal as a GOST body paragraph
// (justified, first-line indent), three heading levels, three TOC levels, the caption, table text and a bordered table.
func styles(f values.Formatting) string {
	font := attr(f.FontName)
	size := f.FontSizePt * 2
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:styles xmlns:w="` + wordNS + `">`)
	fmt.Fprintf(&b, `<w:docDefaults><w:rPrDefault><w:rPr>`+
		`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`+
		`<w:sz w:val="%d"/><w:szCs w:val="%d"/><w:lang w:val="ru-RU"/>`+
		`</w:rPr></w:rPrDefault>`+
		`<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`,
		font, font, font, font, size, size, lineValue(f.LineSpacingPct))
	fmt.Fprintf(&b, `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/>`+
		`<w:pPr><w:widowControl/><w:ind w:firstLine="%d"/><w:jc w:val="both"/></w:pPr></w:style>`, twipsFromTenthMM(f.IndentTenthMM))
	b.WriteString(headingStyle(1, size+4, "") + headingStyle(2, size, "") + headingStyle(3, size, "<w:i/>"))
	for level := 1; level <= values.MaxHeadingLevel; level++ {
		fmt.Fprintf(&b, `<w:style w:type="paragraph" w:styleId="%s%d"><w:name w:val="toc %d"/>`+
			`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/>`+
			`<w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="%d"/></w:tabs>`+
			`<w:ind w:left="%d" w:firstLine="0"/><w:jc w:val="left"/></w:pPr></w:style>`,
			styleTOC, level, level, textWidth(f), (level-1)*tocIndent)
	}
	b.WriteString(`<w:style w:type="paragraph" w:styleId="` + styleCaption + `"><w:name w:val="caption"/>` +
		`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>` +
		`<w:pPr><w:ind w:firstLine="0"/><w:jc w:val="left"/></w:pPr></w:style>`)
	fmt.Fprintf(&b, `<w:style w:type="paragraph" w:customStyle="1" w:styleId="%s"><w:name w:val="Table Text"/>`+
		`<w:basedOn w:val="Normal"/><w:qFormat/>`+
		`<w:pPr><w:spacing w:line="%d" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:jc w:val="left"/></w:pPr></w:style>`,
		styleCell, singleLine)
	b.WriteString(`<w:style w:type="table" w:styleId="` + styleTable + `"><w:name w:val="Table Grid"/>` +
		`<w:tblPr>` + tableBorders + `<w:tblCellMar><w:left w:w="108" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr>` +
		`</w:style>`)
	b.WriteString(`</w:styles>`)
	return b.String()
}

// tableBorders: single black lines around and between cells. Tables repeat them inline for readers that ignore
// table styles.
const tableBorders = `<w:tblBorders>` +
	`<w:top w:val="single" w:sz="4" w:space="0" w:color="000000"/>` +
	`<w:left w:val="single" w:sz="4" w:space="0" w:color="000000"/>` +
	`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="000000"/>` +
	`<w:right w:val="single" w:sz="4" w:space="0" w:color="000000"/>` +
	`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="000000"/>` +
	`<w:insideV w:val="single" w:sz="4" w:space="0" w:color="000000"/>` +
	`</w:tblBorders>`

func headingStyle(level, size int, extra string) string {
	return fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="%s%d"><w:name w:val="heading %d"/>`+
		`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>`+
		`<w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:jc w:val="left"/><w:outlineLvl w:val="%d"/></w:pPr>`+
		`<w:rPr><w:b/>%s<w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`, styleHeading, level, level, level-1, extra, size, size)
}
