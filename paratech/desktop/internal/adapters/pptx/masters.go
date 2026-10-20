package pptx

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

// palette is the look of a template, in the colours of the application (docs/design/concept-strict.html). The
// slides use bg, text, muted and accent directly; scheme is the theme colour scheme for what the student adds.
type palette struct {
	bg, text, muted, accent string
	dark                    bool // the master maps the background to dk1 and the text to lt1
	scheme                  colorScheme
}

// colorScheme is the 12 colours of a theme: dk1, lt1, dk2, lt2, accent1..6, hlink, folHlink.
type colorScheme [12]string

var (
	lightAccents = [6]string{"A6541E", "2C6644", "2B5788", "735000", "B23A30", "4E5158"}
	darkAccents  = [6]string{"DE9A68", "8FCDA7", "98BBE4", "DFC07A", "E8766B", "B4B7BD"}
)

func scheme(dk1, lt1, dk2, lt2 string, accents [6]string, hlink, folHlink string) colorScheme {
	return colorScheme{dk1, lt1, dk2, lt2, accents[0], accents[1], accents[2], accents[3], accents[4], accents[5], hlink, folHlink}
}

var palettes = map[values.PresentationTemplate]palette{
	values.PresentationPaper: {bg: "F4F4F1", text: "1A1B1E", muted: "5A5D64", accent: "A6541E",
		scheme: scheme("1A1B1E", "F4F4F1", "1F2023", "EBEBE7", lightAccents, "2B5788", "5A5D64")},
	values.PresentationWhite: {bg: "FFFFFF", text: "1A1B1E", muted: "5A5D64", accent: "A6541E",
		scheme: scheme("1A1B1E", "FFFFFF", "1F2023", "EBEBE7", lightAccents, "2B5788", "5A5D64")},
	values.PresentationGraphite: {bg: "1F2023", text: "ECEBE7", muted: "9EA1A8", accent: "DE9A68", dark: true,
		scheme: scheme("1F2023", "ECEBE7", "18191B", "9EA1A8", darkAccents, "98BBE4", "9EA1A8")},
}

// Sizes in EMU (914400 per inch, 12700 per point): a 16:9 slide and a portrait notes page.
const (
	slideW = 12192000
	slideH = 6858000
	notesW = 6858000
	notesH = 9144000
)

// box is the position and size of a shape.
type box struct{ x, y, w, h int }

// Geometry of the slides; the layouts repeat it so a slide the student adds looks the same.
var (
	marginX  = 838200
	contentW = slideW - 2*marginX
	columnW  = (contentW - 457200) / 2

	coverTitle    = box{marginX, 1645920, contentW, 1828800}
	coverAccent   = box{marginX, 3611880, 914400, 45720}
	coverSubtitle = box{marginX, 3886200, contentW, 1828800}

	pageTitle   = box{marginX, 457200, contentW, 1097280}
	pageAccent  = box{marginX, 1645920, 731520, 38100}
	pageBody    = box{marginX, 1920240, contentW, 4480560}
	columnLeft  = box{marginX, 1920240, columnW, 4480560}
	columnRight = box{marginX + columnW + 457200, 1920240, columnW, 4480560}

	notesImage = box{685800, 1143000, 5486400, 3086100}
	notesBody  = box{685800, 4400550, 5486400, 3600450}
)

// Theme fonts. Golos Text is the font of the application; pitchFamily 34 (variable-pitch sans) and charset 0 make an
// application that does not have it fall back to a system sans. Cyrillic is drawn with the latin font.
const (
	themeFont  = `<a:latin typeface="Golos Text" pitchFamily="34" charset="0"/><a:ea typeface=""/><a:cs typeface=""/>`
	majorFonts = `<a:latin typeface="+mj-lt"/><a:ea typeface="+mj-ea"/><a:cs typeface="+mj-cs"/>`
	minorFonts = `<a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/><a:cs typeface="+mn-cs"/>`
)

// theme renders a theme part. PowerPoint repairs a file whose format scheme lists fewer than three styles of each
// kind, so every list has three.
func theme(name string, c colorScheme) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="` + name + `">`)
	b.WriteString(`<a:themeElements><a:clrScheme name="` + name + `">`)
	for i, tag := range []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"} {
		fmt.Fprintf(&b, `<a:%s><a:srgbClr val="%s"/></a:%s>`, tag, c[i], tag)
	}
	b.WriteString(`</a:clrScheme>`)
	b.WriteString(`<a:fontScheme name="paratech"><a:majorFont>` + themeFont + `</a:majorFont>` +
		`<a:minorFont>` + themeFont + `</a:minorFont></a:fontScheme>`)
	b.WriteString(`<a:fmtScheme name="paratech"><a:fillStyleLst>` + fills + `</a:fillStyleLst><a:lnStyleLst>`)
	for _, w := range []int{6350, 12700, 19050} {
		fmt.Fprintf(&b, `<a:ln w="%d" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`+
			`<a:prstDash val="solid"/><a:miter lim="800000"/></a:ln>`, w)
	}
	b.WriteString(`</a:lnStyleLst><a:effectStyleLst>` + strings.Repeat(`<a:effectStyle><a:effectLst/></a:effectStyle>`, 3) +
		`</a:effectStyleLst><a:bgFillStyleLst>` + fills + `</a:bgFillStyleLst></a:fmtScheme>`)
	b.WriteString(`</a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`)
	return b.String()
}

const fills = `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"><a:tint val="50000"/></a:schemeClr></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"><a:shade val="80000"/></a:schemeClr></a:solidFill>`

// layout is one slide layout: its type, name and placeholders.
type layout struct {
	typ, name    string
	placeholders []placeholder
}

type placeholder struct {
	name, ph, anchor string
	at               box
}

// Layouts, numbered from 1 in this order; slide() picks them by number.
const (
	layoutCover   = 1
	layoutContent = 2
	layoutColumns = 3
)

var (
	titleCover  = placeholder{"Title", `type="ctrTitle"`, "b", coverTitle}
	subtitleCov = placeholder{"Subtitle", `type="subTitle" idx="1"`, "t", coverSubtitle}
	titlePage   = placeholder{"Title", `type="title"`, "b", pageTitle}
	bodyPage    = placeholder{"Content", `idx="1"`, "t", pageBody}
	bodyLeft    = placeholder{"Left", `idx="1" sz="half"`, "t", columnLeft}
	bodyRight   = placeholder{"Right", `idx="2" sz="half"`, "t", columnRight}

	layouts = []layout{
		{"title", "Title Slide", []placeholder{titleCover, subtitleCov}},
		{"obj", "Title and Content", []placeholder{titlePage, bodyPage}},
		{"twoObj", "Two Content", []placeholder{titlePage, bodyLeft, bodyRight}},
	}
)

const (
	spTreeStart = `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
	emptyParagraph = `<a:p><a:endParaRPr lang="ru-RU" dirty="0"/></a:p>`
	masterMapping  = `<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`
)

func slideLayout(l layout) string {
	var b strings.Builder
	fmt.Fprintf(&b, xmlHeader+`<p:sldLayout %s type="%s" preserve="1"><p:cSld name="%s">`+spTreeStart, pmlNS, l.typ, l.name)
	for i, p := range l.placeholders {
		shape(&b, i+2, p, emptyParagraph)
	}
	b.WriteString(`</p:spTree></p:cSld>` + masterMapping + `</p:sldLayout>`)
	return b.String()
}

func masterRels() string {
	list := make([]rel, 0, len(layouts)+1)
	for i := range layouts {
		list = append(list, rel{relLayout, fmt.Sprintf("../slideLayouts/slideLayout%d.xml", i+1)})
	}
	return rels(append(list, rel{relTheme, "../theme/theme1.xml"}))
}

func slideMaster(pal palette) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<p:sldMaster ` + pmlNS + `><p:cSld>`)
	fmt.Fprintf(&b, `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`, pal.bg)
	b.WriteString(spTreeStart)
	shape(&b, 2, placeholder{"Title", `type="title"`, "b", pageTitle}, emptyParagraph)
	shape(&b, 3, placeholder{"Text", `type="body" idx="1"`, "t", pageBody}, emptyParagraph)
	b.WriteString(`</p:spTree></p:cSld>`)

	if pal.dark {
		b.WriteString(`<p:clrMap bg1="dk1" tx1="lt1" bg2="dk2" tx2="lt2"`)
	} else {
		b.WriteString(`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2"`)
	}
	b.WriteString(` accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5"` +
		` accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst>`)
	for i := range layouts {
		fmt.Fprintf(&b, `<p:sldLayoutId id="%d" r:id="rId%d"/>`, masterID+1+i, i+1)
	}
	b.WriteString(`</p:sldLayoutIdLst><p:txStyles>`)

	const pPr = ` algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangingPunct="1"`
	b.WriteString(`<p:titleStyle><a:lvl1pPr` + pPr + `><a:lnSpc><a:spcPct val="90000"/></a:lnSpc>` +
		`<a:spcBef><a:spcPct val="0"/></a:spcBef><a:buNone/><a:defRPr sz="3600" b="1" kern="1200">` +
		`<a:solidFill><a:schemeClr val="tx1"/></a:solidFill>` + majorFonts + `</a:defRPr></a:lvl1pPr></p:titleStyle>`)
	fmt.Fprintf(&b, `<p:bodyStyle><a:lvl1pPr marL="%d" indent="-%d"`+pPr+`><a:lnSpc><a:spcPct val="100000"/></a:lnSpc>`+
		`<a:spcBef><a:spcPts val="1000"/></a:spcBef><a:buClr><a:srgbClr val="%s"/></a:buClr><a:buFont typeface="Arial"/>`+
		`<a:buChar char="%s"/><a:defRPr sz="%d" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill>`+
		minorFonts+`</a:defRPr></a:lvl1pPr></p:bodyStyle>`, bulletIndent, bulletIndent, pal.muted, bulletChar, bulletSize)
	b.WriteString(`<p:otherStyle><a:defPPr><a:defRPr lang="ru-RU"/></a:defPPr><a:lvl1pPr marL="0"` + pPr + `>` +
		`<a:defRPr sz="1800" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill>` + minorFonts +
		`</a:defRPr></a:lvl1pPr></p:otherStyle></p:txStyles></p:sldMaster>`)
	return b.String()
}

// notesMaster has its own light theme (theme2), so the notes read as dark text on white in every template.
var notesMaster = func() string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<p:notesMaster ` + pmlNS + `><p:cSld>` +
		`<p:bg><p:bgPr><a:solidFill><a:schemeClr val="bg1"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>` + spTreeStart)
	notesShapes(&b, emptyParagraph)
	b.WriteString(`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1"` +
		` accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink"` +
		` folHlink="folHlink"/><p:notesStyle><a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1"` +
		` latinLnBrk="0" hangingPunct="1"><a:defRPr sz="1200" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill>` +
		minorFonts + `</a:defRPr></a:lvl1pPr></p:notesStyle></p:notesMaster>`)
	return b.String()
}()

// notesShapes writes the slide image (id 2) and the notes text (id 3) of a notes page.
func notesShapes(b *strings.Builder, paragraphs string) {
	fmt.Fprintf(b, `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/>`+
		`</p:cNvSpPr><p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr>%s<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`+
		`<a:noFill/><a:ln w="12700"><a:solidFill><a:srgbClr val="5A5D64"/></a:solidFill></a:ln></p:spPr></p:sp>`, xfrm(notesImage))
	fmt.Fprintf(b, `<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>`+
		`<p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr>%s</p:spPr>`+
		`<p:txBody><a:bodyPr vert="horz" wrap="square" rtlCol="0"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
		xfrm(notesBody), paragraphs)
}

func xfrm(at box) string {
	return fmt.Sprintf(`<a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, at.x, at.y, at.w, at.h)
}

// shape writes a placeholder with an explicit position and the given paragraphs (at least one).
func shape(b *strings.Builder, id int, p placeholder, paragraphs string) {
	fmt.Fprintf(b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s %d"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>`+
		`<p:nvPr><p:ph %s/></p:nvPr></p:nvSpPr><p:spPr>%s</p:spPr><p:txBody>`+
		`<a:bodyPr vert="horz" wrap="square" lIns="0" tIns="0" rIns="0" bIns="0" rtlCol="0" anchor="%s"><a:normAutofit/></a:bodyPr>`+
		`<a:lstStyle/>%s</p:txBody></p:sp>`, id, p.name, id-1, p.ph, xfrm(p.at), p.anchor, paragraphs)
}

// accentBar writes the thin accent line, the only use of the accent colour on a slide.
func accentBar(b *strings.Builder, id int, at box, color string) {
	fmt.Fprintf(b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Accent %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>%s`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln>`+
		`</p:spPr></p:sp>`, id, id-1, xfrm(at), color)
}
