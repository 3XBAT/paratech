package docx

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const (
	wordNS = `http://schemas.openxmlformats.org/wordprocessingml/2006/main`
	relNS  = `http://schemas.openxmlformats.org/officeDocument/2006/relationships`

	footerPart      = "footer1.xml" // the page number
	firstFooterPart = "footer2.xml" // empty: the title page has no number
	footerRel       = "rId4"
	firstFooterRel  = "rId5"
)

const contentTypes = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/word/` + footerPart + `" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>` +
	`<Override PartName="/word/` + firstFooterPart + `" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>` +
	`</Types>`

const packageRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + relNS + `/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

const documentRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + relNS + `/styles" Target="styles.xml"/>` +
	`<Relationship Id="rId2" Type="` + relNS + `/settings" Target="settings.xml"/>` +
	`<Relationship Id="rId3" Type="` + relNS + `/numbering" Target="numbering.xml"/>` +
	`<Relationship Id="` + footerRel + `" Type="` + relNS + `/footer" Target="` + footerPart + `"/>` +
	`<Relationship Id="` + firstFooterRel + `" Type="` + relNS + `/footer" Target="` + firstFooterPart + `"/>` +
	`</Relationships>`

// settings: Word 2013+ layout (no compatibility mode; a page break leaves no empty line on the next page) and, with a
// table of contents, updateFields so Word refreshes the TOC with page numbers on open.
func settings(toc bool) string {
	update := ""
	if toc {
		update = `<w:updateFields w:val="true"/>`
	}
	return xmlHeader + `<w:settings xmlns:w="` + wordNS + `">` +
		`<w:defaultTabStop w:val="709"/>` + update +
		`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
		`</w:settings>`
}

// A footer paragraph is centred and has no first-line indent (Normal has one).
const footerParagraph = `<w:pPr><w:ind w:firstLine="0"/><w:jc w:val="center"/></w:pPr>`

// footer shows the page number through a PAGE field; "1" is its cached result.
const footer = xmlHeader + `<w:ftr xmlns:w="` + wordNS + `"><w:p>` + footerParagraph +
	`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>` +
	`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
	`</w:p></w:ftr>`

const firstFooter = xmlHeader + `<w:ftr xmlns:w="` + wordNS + `"><w:p>` + footerParagraph + `</w:p></w:ftr>`

// Numbering definitions (abstractNumId) and the instance of every bullet list.
const (
	abstractBullet       = 0
	abstractDecimal      = 1 // «1)», GOST enumerations
	abstractBibliography = 2 // «1.»
	bulletNum            = 1 // all bullet lists share one instance: they have no counter
	firstListNum         = 2 // numbered lists and bibliographies get their own instances from here
	bulletText           = "–"
)

// numbering returns numbering.xml: the definitions, the bullet instance and one instance per numbered list (lists[i]
// is the definition of instance firstListNum+i) restarting at 1. A list item starts at the paragraph indent, the text
// follows after a space and wraps to the margin, as GOST paragraphs do.
func numbering(f values.Formatting, lists []int) string {
	ind := twipsFromTenthMM(f.IndentTenthMM)
	lvl := func(id int, format, txt string) string {
		return fmt.Sprintf(`<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="singleLevel"/>`+
			`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:suff w:val="space"/><w:lvlText w:val="%s"/>`+
			`<w:lvlJc w:val="left"/><w:pPr><w:ind w:left="0" w:firstLine="%d"/></w:pPr></w:lvl></w:abstractNum>`,
			id, format, txt, ind)
	}
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:numbering xmlns:w="` + wordNS + `">`)
	b.WriteString(lvl(abstractBullet, "bullet", bulletText))
	b.WriteString(lvl(abstractDecimal, "decimal", "%1)"))
	b.WriteString(lvl(abstractBibliography, "decimal", "%1."))
	fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/></w:num>`, bulletNum, abstractBullet)
	for i, abstract := range lists {
		fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/>`+
			`<w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, firstListNum+i, abstract)
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}
