// Package pptx renders a presentation (values.Presentation) as a PowerPoint file (TASK-106): a minimal but complete
// PresentationML package (presentation, one theme, one slide master with three layouts, a notes master, the slides
// with their speaker notes and the document properties), zipped with archive/zip. Standard library only; PowerPoint
// and LibreOffice Impress open it. The output is deterministic: the same presentation gives the same bytes.
//
// Every text is data: characters that XML cannot hold and bidi controls are dropped and the rest is escaped, so a
// text cannot add markup. The slides are 16:9 and carry explicit positions, colours and sizes, so they look the same
// in both applications whatever their layout inheritance does. No images.
package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

// ErrRender means the package could not be assembled.
var ErrRender = errors.New("pptx: the presentation cannot be rendered")

// Renderer renders presentations. The zero value is ready to use.
type Renderer struct{}

// New returns a Renderer.
func New() Renderer { return Renderer{} }

// part is one entry of the package; contentType is empty for the parts the Default extensions cover.
type part struct {
	name        string
	contentType string
	content     string
}

// Content types and relationship types of the parts.
const (
	ctPresentation = "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"
	ctSlide        = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"
	ctLayout       = "application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"
	ctMaster       = "application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"
	ctNotesSlide   = "application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"
	ctNotesMaster  = "application/vnd.openxmlformats-officedocument.presentationml.notesMaster+xml"
	ctTheme        = "application/vnd.openxmlformats-officedocument.theme+xml"
	ctPresProps    = "application/vnd.openxmlformats-officedocument.presentationml.presProps+xml"
	ctViewProps    = "application/vnd.openxmlformats-officedocument.presentationml.viewProps+xml"
	ctTableStyles  = "application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml"
	ctCore         = "application/vnd.openxmlformats-package.core-properties+xml"
	ctApp          = "application/vnd.openxmlformats-officedocument.extended-properties+xml"

	relNS          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	relDocument    = relNS + "officeDocument"
	relCore        = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	relApp         = relNS + "extended-properties"
	relMaster      = relNS + "slideMaster"
	relLayout      = relNS + "slideLayout"
	relSlide       = relNS + "slide"
	relNotesSlide  = relNS + "notesSlide"
	relNotesMaster = relNS + "notesMaster"
	relTheme       = relNS + "theme"
	relPresProps   = relNS + "presProps"
	relViewProps   = relNS + "viewProps"
	relTableStyles = relNS + "tableStyles"
)

// Render returns the .pptx bytes of p. An invalid presentation is an error matching both ErrRender and
// values.ErrInvalidPresentation.
func (Renderer) Render(ctx context.Context, p values.Presentation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}

	parts, err := assemble(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}

	// One fixed modification time for every entry keeps the output deterministic.
	modified := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, pt := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: pt.name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRender, err)
		}
		if _, err := w.Write([]byte(pt.content)); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRender, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	return buf.Bytes(), nil
}

// assemble returns every part of the package, [Content_Types].xml first.
func assemble(ctx context.Context, p values.Presentation) ([]part, error) {
	pal := palettes[p.Template.OrDefault()]
	n := len(p.Slides)

	parts := []part{
		{"_rels/.rels", "", rels([]rel{
			{relDocument, "ppt/presentation.xml"},
			{relCore, "docProps/core.xml"},
			{relApp, "docProps/app.xml"},
		})},
		{"docProps/core.xml", ctCore, coreProps(oneLine(p.Slides[0].Title))},
		{"docProps/app.xml", ctApp, appProps(n)},
		{"ppt/presentation.xml", ctPresentation, presentation(n)},
		{"ppt/_rels/presentation.xml.rels", "", presentationRels(n)},
		{"ppt/presProps.xml", ctPresProps, presProps},
		{"ppt/viewProps.xml", ctViewProps, viewProps},
		{"ppt/tableStyles.xml", ctTableStyles, tableStyles},
		{"ppt/theme/theme1.xml", ctTheme, theme("paratech "+string(p.Template.OrDefault()), pal.scheme)},
		{"ppt/theme/theme2.xml", ctTheme, theme("paratech notes", palettes[values.PresentationWhite].scheme)},
		{"ppt/slideMasters/slideMaster1.xml", ctMaster, slideMaster(pal)},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", "", masterRels()},
	}
	for i, l := range layouts {
		name := "slideLayout" + strconv.Itoa(i+1) + ".xml"
		parts = append(parts,
			part{"ppt/slideLayouts/" + name, ctLayout, slideLayout(l)},
			part{"ppt/slideLayouts/_rels/" + name + ".rels", "", rels([]rel{{relMaster, "../slideMasters/slideMaster1.xml"}})},
		)
	}
	parts = append(parts,
		part{"ppt/notesMasters/notesMaster1.xml", ctNotesMaster, notesMaster},
		part{"ppt/notesMasters/_rels/notesMaster1.xml.rels", "", rels([]rel{{relTheme, "../theme/theme2.xml"}})},
	)

	for i, s := range p.Slides {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		num := strconv.Itoa(i + 1)
		content, layout := slide(s, pal)
		parts = append(parts,
			part{"ppt/slides/slide" + num + ".xml", ctSlide, content},
			part{"ppt/slides/_rels/slide" + num + ".xml.rels", "", rels([]rel{
				{relLayout, "../slideLayouts/slideLayout" + strconv.Itoa(layout) + ".xml"},
				{relNotesSlide, "../notesSlides/notesSlide" + num + ".xml"},
			})},
			part{"ppt/notesSlides/notesSlide" + num + ".xml", ctNotesSlide, notesSlide(s.Notes)},
			part{"ppt/notesSlides/_rels/notesSlide" + num + ".xml.rels", "", rels([]rel{
				{relNotesMaster, "../notesMasters/notesMaster1.xml"},
				{relSlide, "../slides/slide" + num + ".xml"},
			})},
		)
	}

	return append([]part{{"[Content_Types].xml", "", contentTypes(parts)}}, parts...), nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// contentTypes lists an Override for every part that has its own content type, so no part can be missed.
func contentTypes(parts []part) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>`)
	for _, p := range parts {
		if p.contentType != "" {
			fmt.Fprintf(&b, `<Override PartName="/%s" ContentType="%s"/>`, p.name, p.contentType)
		}
	}
	b.WriteString(`</Types>`)
	return b.String()
}

// rel is one relationship; its id is rId and its position, from 1.
type rel struct{ typ, target string }

func rels(list []rel) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, r := range list {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="%s" Target="%s"/>`, i+1, r.typ, r.target)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func coreProps(title string) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"` +
		` xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"` +
		` xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`)
	b.WriteString(`<dc:title>`)
	escape(&b, title)
	b.WriteString(`</dc:title><dc:creator>paratech</dc:creator></cp:coreProperties>`)
	return b.String()
}

func appProps(slides int) string {
	return fmt.Sprintf(xmlHeader+`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"`+
		` xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">`+
		`<Application>paratech</Application><PresentationFormat>Widescreen</PresentationFormat>`+
		`<Slides>%d</Slides><Notes>%d</Notes><HiddenSlides>0</HiddenSlides></Properties>`, slides, slides)
}

// Namespaces of the PresentationML parts.
const pmlNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
	` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
	` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

// Ids of the presentation. The master and the layouts share one id space that starts at 2^31; slide ids start at 256.
const (
	masterID     = 2147483648
	firstSlideID = 256
	firstSlideR  = 7 // relationship of the first slide in presentation.xml.rels
)

func presentation(slides int) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<p:presentation ` + pmlNS + ` saveSubsetFonts="1">`)
	fmt.Fprintf(&b, `<p:sldMasterIdLst><p:sldMasterId id="%d" r:id="rId1"/></p:sldMasterIdLst>`, masterID)
	b.WriteString(`<p:notesMasterIdLst><p:notesMasterId r:id="rId2"/></p:notesMasterIdLst><p:sldIdLst>`)
	for i := range slides {
		fmt.Fprintf(&b, `<p:sldId id="%d" r:id="rId%d"/>`, firstSlideID+i, firstSlideR+i)
	}
	fmt.Fprintf(&b, `</p:sldIdLst><p:sldSz cx="%d" cy="%d"/><p:notesSz cx="%d" cy="%d"/>`, slideW, slideH, notesW, notesH)
	b.WriteString(`<p:defaultTextStyle><a:defPPr><a:defRPr lang="ru-RU"/></a:defPPr>` +
		`<a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangingPunct="1">` +
		`<a:defRPr sz="1800" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill>` + minorFonts + `</a:defRPr>` +
		`</a:lvl1pPr></p:defaultTextStyle></p:presentation>`)
	return b.String()
}

func presentationRels(slides int) string {
	list := []rel{
		{relMaster, "slideMasters/slideMaster1.xml"},
		{relNotesMaster, "notesMasters/notesMaster1.xml"},
		{relTheme, "theme/theme1.xml"},
		{relPresProps, "presProps.xml"},
		{relViewProps, "viewProps.xml"},
		{relTableStyles, "tableStyles.xml"},
	}
	for i := range slides {
		list = append(list, rel{relSlide, "slides/slide" + strconv.Itoa(i+1) + ".xml"})
	}
	return rels(list)
}

const (
	presProps = xmlHeader + `<p:presentationPr ` + pmlNS + `/>`
	viewProps = xmlHeader + `<p:viewPr ` + pmlNS + `><p:normalViewPr><p:restoredLeft sz="15620"/>` +
		`<p:restoredTop sz="94660"/></p:normalViewPr><p:gridSpacing cx="72008" cy="72008"/></p:viewPr>`
	tableStyles = xmlHeader + `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`
)
