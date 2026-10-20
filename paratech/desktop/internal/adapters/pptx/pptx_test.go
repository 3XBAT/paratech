package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

// sample has every slide kind, multi-line notes and the characters XML must escape.
func sample(tpl values.PresentationTemplate) values.Presentation {
	return values.Presentation{Template: tpl, Slides: []values.Slide{
		{Kind: values.SlideTitle, Title: "Пределы последовательностей",
			Subtitle: "Иванов Иван Иванович, ИВТ-21\nМатематический анализ\n20.10.2026",
			Notes:    "Добрый день! Меня зовут Иван.\n\nСегодня — о пределах."},
		{Kind: values.SlideBullets, Title: "Определение: a < b & b > c",
			Bullets: []string{
				"Последовательность — функция натурального аргумента",
				`Предел «a» — число, к которому "стремятся" члены`,
				"Запись: lim aₙ = a при n → ∞",
				"Сходящаяся последовательность ограничена",
				"Предел единственен",
				"Пример: 1/n → 0, а 'n' не сходится",
			},
			Notes: "Сначала определение.\nПотом примеры <на доске>."},
		{Kind: values.SlideTwoColumns, Title: "Сходимость и расходимость",
			Left:  values.SlideColumn{Heading: "Сходятся", Bullets: []string{"1/n", "(−1)ⁿ/n", "q ⁿ при |q| < 1"}},
			Right: values.SlideColumn{Heading: "Расходятся", Bullets: []string{"n", "(−1)ⁿ"}},
			Notes: "Сравните столбцы."},
		{Kind: values.SlideConclusion, Title: "Выводы",
			Bullets: []string{"Предел описывает поведение «на бесконечности»", "Спасибо за внимание!"}},
	}}
}

func render(t *testing.T, p values.Presentation) []byte {
	t.Helper()
	b, err := New().Render(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// entries opens the package and returns every entry by name, and the names in order.
func entries(t *testing.T, b []byte) (map[string]string, []string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip archive: %v", err)
	}
	out := map[string]string{}
	var names []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := out[f.Name]; dup {
			t.Fatalf("duplicate entry %s", f.Name)
		}
		out[f.Name] = string(data)
		names = append(names, f.Name)
	}
	return out, names
}

// wellFormed parses an XML part to the end.
func wellFormed(t *testing.T, name, part string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(part))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("%s is not well-formed XML: %v\n%s", name, err, part)
		}
	}
}

type contentTypesXML struct {
	Defaults []struct {
		Extension string `xml:"Extension,attr"`
	} `xml:"Default"`
	Overrides []struct {
		PartName    string `xml:"PartName,attr"`
		ContentType string `xml:"ContentType,attr"`
	} `xml:"Override"`
}

type relsXML struct {
	Rels []struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

func decode(t *testing.T, name, part string, v any) {
	t.Helper()
	if err := xml.Unmarshal([]byte(part), v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// relsSource returns the part a .rels part describes ("" for the package).
func relsSource(name string) string {
	dir, file := path.Split(name)
	return path.Join(path.Dir(strings.TrimSuffix(dir, "/")), strings.TrimSuffix(file, ".rels"))
}

// checkPackage checks the structure of the package and returns its parts.
func checkPackage(t *testing.T, b []byte) map[string]string {
	t.Helper()
	parts, names := entries(t, b)
	if names[0] != "[Content_Types].xml" {
		t.Fatalf("first entry = %s", names[0])
	}
	for name, content := range parts {
		wellFormed(t, name, content)
		if !strings.HasPrefix(content, xmlHeader) {
			t.Fatalf("%s has no XML declaration", name)
		}
	}

	// Every part has a content type, and every override names a part.
	var ct contentTypesXML
	decode(t, "[Content_Types].xml", parts["[Content_Types].xml"], &ct)
	defaults := map[string]bool{}
	for _, d := range ct.Defaults {
		defaults[d.Extension] = true
	}
	overridden := map[string]bool{}
	for _, o := range ct.Overrides {
		name := strings.TrimPrefix(o.PartName, "/")
		if _, ok := parts[name]; !ok {
			t.Fatalf("override for a missing part %s", o.PartName)
		}
		if overridden[name] {
			t.Fatalf("two overrides for %s", name)
		}
		overridden[name] = true
	}
	for name := range parts {
		if name == "[Content_Types].xml" || strings.HasSuffix(name, ".rels") {
			continue
		}
		if !overridden[name] {
			t.Fatalf("%s has no content type override", name)
		}
	}
	if !defaults["rels"] || !defaults["xml"] {
		t.Fatalf("defaults = %v", defaults)
	}

	// Every relationship target exists, ids are unique, and every part but the content types is reachable.
	reached := map[string]bool{"[Content_Types].xml": true}
	for name, content := range parts {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		reached[name] = true
		src := relsSource(name)
		if src != "." {
			if _, ok := parts[src]; !ok {
				t.Fatalf("%s describes a missing part %s", name, src)
			}
		}
		var r relsXML
		decode(t, name, content, &r)
		ids := map[string]bool{}
		for _, rel := range r.Rels {
			if ids[rel.ID] {
				t.Fatalf("%s: duplicate id %s", name, rel.ID)
			}
			ids[rel.ID] = true
			target := path.Join(path.Dir(src), rel.Target)
			if _, ok := parts[target]; !ok {
				t.Fatalf("%s: %s points to a missing part %s", name, rel.ID, target)
			}
			reached[target] = true
		}
	}
	for name := range parts {
		if !reached[name] {
			t.Fatalf("%s is not reachable by a relationship", name)
		}
	}
	return parts
}

var (
	shapeID   = regexp.MustCompile(`<p:cNvPr id="(\d+)"`)
	slideIDs  = regexp.MustCompile(`<p:sldId id="(\d+)" r:id="(rId\d+)"/>`)
	layoutIDs = regexp.MustCompile(`<p:sldLayoutId id="(\d+)"`)
)

func TestRenderPackageStructure(t *testing.T) {
	p := sample(values.PresentationPaper)
	parts := checkPackage(t, render(t, p))

	for _, name := range []string{
		"docProps/core.xml", "docProps/app.xml", "ppt/presentation.xml", "ppt/presProps.xml", "ppt/viewProps.xml",
		"ppt/tableStyles.xml", "ppt/theme/theme1.xml", "ppt/theme/theme2.xml", "ppt/slideMasters/slideMaster1.xml",
		"ppt/notesMasters/notesMaster1.xml", "ppt/slideLayouts/slideLayout1.xml", "ppt/slideLayouts/slideLayout3.xml",
	} {
		if _, ok := parts[name]; !ok {
			t.Errorf("missing part %s", name)
		}
	}

	// The slides are listed in order, with ids from 256, each pointing at its own slide part.
	var pres relsXML
	decode(t, "presentation rels", parts["ppt/_rels/presentation.xml.rels"], &pres)
	targets := map[string]string{}
	for _, r := range pres.Rels {
		targets[r.ID] = r.Target
	}
	m := slideIDs.FindAllStringSubmatch(parts["ppt/presentation.xml"], -1)
	if len(m) != len(p.Slides) {
		t.Fatalf("%d slide ids, want %d", len(m), len(p.Slides))
	}
	for i, s := range m {
		if want := 256 + i; s[1] != strconv.Itoa(want) {
			t.Errorf("slide %d id = %s, want %d", i+1, s[1], want)
		}
		if want := "slides/slide" + strconv.Itoa(i+1) + ".xml"; targets[s[2]] != want {
			t.Errorf("slide %d target = %s, want %s", i+1, targets[s[2]], want)
		}
	}
	if !strings.Contains(parts["ppt/presentation.xml"], `<p:sldSz cx="12192000" cy="6858000"/>`) {
		t.Error("slide size is not 16:9")
	}

	// Master and layout ids share one space from 2^31.
	ids := []string{"2147483648"}
	for _, l := range layoutIDs.FindAllStringSubmatch(parts["ppt/slideMasters/slideMaster1.xml"], -1) {
		ids = append(ids, l[1])
	}
	if want := []string{"2147483648", "2147483649", "2147483650", "2147483651"}; !slices.Equal(ids, want) {
		t.Errorf("master and layout ids = %v, want %v", ids, want)
	}

	// Every format list of the themes has three styles, or PowerPoint repairs the file.
	for _, name := range []string{"ppt/theme/theme1.xml", "ppt/theme/theme2.xml"} {
		th := parts[name]
		for _, tag := range []string{"fillStyleLst", "lnStyleLst", "effectStyleLst", "bgFillStyleLst"} {
			if n := styleCount(t, th, tag); n != 3 {
				t.Errorf("%s: %s has %d styles, want 3", name, tag, n)
			}
		}
		if !strings.Contains(th, `<a:latin typeface="Golos Text" pitchFamily="34" charset="0"/>`) {
			t.Errorf("%s: no Golos Text", name)
		}
	}

	// Shape ids are unique within every slide, layout, master and notes page; 1 is the shape tree.
	for name, content := range parts {
		if !strings.HasSuffix(name, ".xml") || !strings.Contains(content, "<p:spTree>") {
			continue
		}
		seen := map[string]bool{}
		for i, id := range shapeID.FindAllStringSubmatch(content, -1) {
			if i == 0 && id[1] != "1" {
				t.Errorf("%s: first shape id = %s", name, id[1])
			}
			if seen[id[1]] {
				t.Errorf("%s: duplicate shape id %s", name, id[1])
			}
			seen[id[1]] = true
		}
	}
}

// styleCount counts the direct children of the first element named tag.
func styleCount(t *testing.T, part, tag string) int {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(part))
	depth, n := -1, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return n
		}
		if err != nil {
			t.Fatal(err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			if depth >= 0 {
				depth++
				if depth == 1 {
					n++
				}
			} else if e.Name.Local == tag {
				depth = 0
			}
		case xml.EndElement:
			if depth == 0 {
				return n
			}
			if depth > 0 {
				depth--
			}
		}
	}
}

// paragraphs returns the text of every a:p of a part, its runs joined.
func paragraphs(t *testing.T, part string) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(part))
	var out []string
	var cur strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			if e.Name.Local == "p" && e.Name.Space == "http://schemas.openxmlformats.org/drawingml/2006/main" {
				cur.Reset()
			}
			inText = e.Name.Local == "t"
		case xml.EndElement:
			if e.Name.Local == "p" && e.Name.Space == "http://schemas.openxmlformats.org/drawingml/2006/main" {
				out = append(out, cur.String())
			}
			inText = false
		case xml.CharData:
			if inText {
				cur.Write(e)
			}
		}
	}
}

func TestRenderSlidesAndNotes(t *testing.T) {
	p := sample(values.PresentationPaper)
	parts := checkPackage(t, render(t, p))

	wantLayout := []string{"slideLayout1.xml", "slideLayout2.xml", "slideLayout3.xml", "slideLayout2.xml"}
	for i, s := range p.Slides {
		num := strconv.Itoa(i + 1)
		got := paragraphs(t, parts["ppt/slides/slide"+num+".xml"])
		var want []string
		switch s.Kind {
		case values.SlideTitle:
			want = append([]string{s.Title}, strings.Split(s.Subtitle, "\n")...)
		case values.SlideTwoColumns:
			want = append(append(append(append([]string{s.Title}, s.Left.Heading), s.Left.Bullets...), s.Right.Heading), s.Right.Bullets...)
		default:
			want = append([]string{s.Title}, s.Bullets...)
		}
		if !slices.Equal(got, want) {
			t.Errorf("slide %d text = %q, want %q", i+1, got, want)
		}
		if rels := parts["ppt/slides/_rels/slide"+num+".xml.rels"]; !strings.Contains(rels, "../slideLayouts/"+wantLayout[i]) {
			t.Errorf("slide %d layout: %s", i+1, rels)
		}

		notes := parts["ppt/notesSlides/notesSlide"+num+".xml"]
		if !strings.Contains(notes, `<p:ph type="sldImg"/>`) || !strings.Contains(notes, `<p:ph type="body" idx="1"/>`) {
			t.Errorf("notes %d have no slide image or body placeholder", i+1)
		}
		gotNotes := paragraphs(t, notes)
		wantNotes := []string{""}
		if s.Notes != "" {
			wantNotes = strings.Split(s.Notes, "\n")
		}
		if !slices.Equal(gotNotes, wantNotes) {
			t.Errorf("notes %d = %q, want %q", i+1, gotNotes, wantNotes)
		}
	}

	if got := paragraphs(t, parts["ppt/slides/slide2.xml"]); !strings.Contains(parts["ppt/slides/slide2.xml"], `<a:buChar char="•"/>`) || len(got) != 7 {
		t.Errorf("bullets slide: %q", got)
	}
	if strings.Contains(parts["ppt/slides/slide4.xml"], "<a:buChar") {
		t.Error("the conclusion lines have bullets")
	}
	if !strings.Contains(parts["docProps/app.xml"], "<Slides>4</Slides>") {
		t.Errorf("app.xml: %s", parts["docProps/app.xml"])
	}
}

func TestRenderEscapesText(t *testing.T) {
	p := values.Presentation{Slides: []values.Slide{
		{Kind: values.SlideBullets, Title: `<b>&"'</b>`,
			Bullets: []string{"a\x00b\x1fc\uFFFEd", "e\u202Ef", "line\nbreak\tand tab", "\xffbad utf-8"},
			Notes:   "<p:sp/>\r\nвторая & строка\rтретья"},
	}}
	parts := checkPackage(t, render(t, p))
	slide := parts["ppt/slides/slide1.xml"]
	if strings.Contains(slide, "<b>") || !strings.Contains(slide, "&lt;b&gt;&amp;&#34;&#39;&lt;/b&gt;") {
		t.Fatalf("title is not escaped:\n%s", slide)
	}
	want := []string{`<b>&"'</b>`, "abcd", "ef", "line break and tab", "bad utf-8"}
	if got := paragraphs(t, slide); !slices.Equal(got, want) {
		t.Errorf("slide text = %q, want %q", got, want)
	}
	if got, want := paragraphs(t, parts["ppt/notesSlides/notesSlide1.xml"]), []string{"<p:sp/>", "вторая & строка", "третья"}; !slices.Equal(got, want) {
		t.Errorf("notes = %q, want %q", got, want)
	}
	if got := paragraphs(t, parts["docProps/core.xml"]); len(got) != 0 || !strings.Contains(parts["docProps/core.xml"], "<dc:title>&lt;b&gt;") {
		t.Errorf("core.xml: %s", parts["docProps/core.xml"])
	}
}

func TestRenderTemplates(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range append(values.PresentationTemplates(), "") {
		t.Run(string(tpl), func(t *testing.T) {
			b := render(t, sample(tpl))
			parts := checkPackage(t, b)
			pal := palettes[tpl.OrDefault()]
			master := parts["ppt/slideMasters/slideMaster1.xml"]
			if !strings.Contains(master, `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="`+pal.bg+`"/>`) {
				t.Errorf("master background is not %s", pal.bg)
			}
			if pal.dark != strings.Contains(master, `bg1="dk1" tx1="lt1"`) {
				t.Errorf("colour map of a dark=%v template: %s", pal.dark, master)
			}
			if !strings.Contains(parts["ppt/slides/slide2.xml"], `<a:srgbClr val="`+pal.accent+`"/>`) {
				t.Errorf("no accent %s on the slide", pal.accent)
			}
			if tpl != "" {
				seen[parts["ppt/slideMasters/slideMaster1.xml"]] = true
			}
		})
	}
	if len(seen) != len(values.PresentationTemplates()) {
		t.Errorf("%d distinct masters for %d templates", len(seen), len(values.PresentationTemplates()))
	}
	if !bytes.Equal(render(t, sample("")), render(t, sample(values.PresentationPaper))) {
		t.Error("the empty template is not paper")
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, b := render(t, sample(values.PresentationGraphite)), render(t, sample(values.PresentationGraphite))
	if !bytes.Equal(a, b) {
		t.Fatal("two renders of the same presentation differ")
	}
}

func TestRenderErrors(t *testing.T) {
	cases := []struct {
		name string
		ctx  func() context.Context
		p    values.Presentation
		want error
	}{
		{"no slides", context.Background, values.Presentation{}, values.ErrInvalidPresentation},
		{"unknown template", context.Background, values.Presentation{Template: "neon", Slides: sample("").Slides}, values.ErrInvalidPresentation},
		{"too many bullets", context.Background, values.Presentation{Slides: []values.Slide{
			{Kind: values.SlideBullets, Title: "x", Bullets: slices.Repeat([]string{"b"}, values.MaxSlideBullets+1)}}}, values.ErrInvalidPresentation},
		{"cancelled", func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, sample(""), context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := New().Render(tc.ctx(), tc.p)
			if b != nil || !errors.Is(err, ErrRender) || !errors.Is(err, tc.want) {
				t.Fatalf("Render() = %d bytes, %v; want ErrRender and %v", len(b), err, tc.want)
			}
		})
	}
}

// TestSample writes the sample of every template for the manual check (docs/qa/run-alpha.md): always into a temporary
// directory, and also into PARATECH_PPTX_SAMPLE_DIR when it is set.
func TestSample(t *testing.T) {
	dirs := []string{t.TempDir()}
	if d := os.Getenv("PARATECH_PPTX_SAMPLE_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	for _, tpl := range values.PresentationTemplates() {
		b := render(t, sample(tpl))
		checkPackage(t, b)
		for _, d := range dirs {
			name := filepath.Join(d, "sample-"+string(tpl)+".pptx")
			if err := os.WriteFile(name, b, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", name)
		}
	}
}
