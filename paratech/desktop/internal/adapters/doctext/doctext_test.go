package doctext

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/library/material"
)

type entry struct {
	name string
	body string
}

func zipOf(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

func docxOf(t *testing.T, body string) []byte {
	t.Helper()
	return zipOf(t, entry{"[Content_Types].xml", "<Types/>"},
		entry{docxBody, `<?xml version="1.0"?><w:document ` + wordNS + `><w:body>` + body + `</w:body></w:document>`})
}

func para(text string) string { return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>` }

const (
	presNS  = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	drawNS  = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`
	relsXML = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">%s</Relationships>`
)

func slide(lines ...string) string {
	var b strings.Builder
	b.WriteString(`<p:sld ` + presNS + ` ` + drawNS + `><p:cSld><p:spTree><p:sp><p:txBody>`)
	for _, l := range lines {
		b.WriteString(`<a:p><a:r><a:t>` + l + `</a:t></a:r></a:p>`)
	}
	b.WriteString(`</p:txBody></p:sp></p:spTree></p:cSld></p:sld>`)
	return b.String()
}

// pptxOf builds a presentation whose slide list names the relationships rids in order; targets maps rId to target.
func pptxOf(t *testing.T, rids []string, targets map[string]string, slides map[string]string) []byte {
	t.Helper()
	var list, rels strings.Builder
	for i, rid := range rids {
		fmt.Fprintf(&list, `<p:sldId id="%d" r:id="%s"/>`, 256+i, rid)
	}
	for rid, target := range targets {
		fmt.Fprintf(&rels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="%s"/>`, rid, target)
	}
	entries := []entry{
		{"[Content_Types].xml", "<Types/>"},
		{pptxMain, `<p:presentation ` + presNS + `><p:sldIdLst>` + list.String() + `</p:sldIdLst></p:presentation>`},
		{pptxMainRels, fmt.Sprintf(relsXML, rels.String())},
	}
	for name, body := range slides {
		entries = append(entries, entry{name, body})
	}
	return zipOf(t, entries...)
}

func extract(t *testing.T, typ material.Type, data []byte) (material.Text, error) {
	t.Helper()
	return New().Extract(context.Background(), typ, data)
}

func TestDOCX(t *testing.T) {
	body := para("Лекция 3.") +
		`<w:p><w:r><w:t>Производная</w:t><w:tab/><w:t xml:space="preserve"> функции</w:t></w:r>` +
		`<w:del><w:r><w:delText>удалено</w:delText></w:r></w:del>` +
		`<w:r><w:instrText>HYPERLINK "http://x.invalid"</w:instrText></w:r></w:p>` +
		`<w:p><w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:t>в рамке</w:t></mc:Choice>` +
		`<mc:Fallback><w:t>в рамке</w:t></mc:Fallback></mc:AlternateContent></w:r></w:p>` +
		para("&lt;b&gt; &amp; конец")
	text, err := extract(t, material.TypeDOCX, docxOf(t, body))
	if err != nil {
		t.Fatal(err)
	}
	want := "[Фрагмент 1]\nЛекция 3.\nПроизводная функции\nв рамке\n<b> & конец"
	if got := text.String(); got != want || text.Truncated() {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestDOCXPages(t *testing.T) {
	body := para("один") +
		`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
		`<w:p><w:r><w:lastRenderedPageBreak/><w:t>два</w:t></w:r></w:p>` + // the same break, recorded by Word
		`<w:p><w:r><w:t>строка</w:t><w:br/><w:t>с переносом</w:t></w:r></w:p>` +
		`<w:p><w:r><w:lastRenderedPageBreak/><w:t>три</w:t></w:r></w:p>`
	text, err := extract(t, material.TypeDOCX, docxOf(t, body))
	if err != nil {
		t.Fatal(err)
	}
	want := "[Страница 1]\nодин\n[Страница 2]\nдва\nстрока с переносом\n[Страница 3]\nтри"
	if got := text.String(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestDOCXWithoutBreaksIsFragmented(t *testing.T) {
	var body strings.Builder
	for i := range 20 {
		body.WriteString(para(fmt.Sprintf("Абзац %d %s", i, strings.Repeat("слово ", 60))))
	}
	text, err := extract(t, material.TypeDOCX, docxOf(t, body.String()))
	if err != nil {
		t.Fatal(err)
	}
	fs := text.Fragments()
	if len(fs) < 2 || fs[0].Locator.Kind != material.LocatorFragment || fs[1].Locator.N != 2 {
		t.Fatalf("fragments = %+v", fs)
	}
}

func TestPPTXSlideOrder(t *testing.T) {
	// The presentation order is rId3, rId2: the second file comes first.
	data := pptxOf(t, []string{"rId3", "rId2"},
		map[string]string{"rId2": "slides/slide1.xml", "rId3": "/ppt/slides/slide2.xml"},
		map[string]string{
			"ppt/slides/slide1.xml": slide("Второй слайд"),
			"ppt/slides/slide2.xml": slide("Первый слайд", "пункт &amp; ещё"),
		})
	text, err := extract(t, material.TypePPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	want := "[Слайд 1]\nПервый слайд\nпункт & ещё\n[Слайд 2]\nВторой слайд"
	if got := text.String(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}

	// A slide without text keeps its number for the next one.
	data = pptxOf(t, []string{"rId1", "rId2"},
		map[string]string{"rId1": "slides/slide1.xml", "rId2": "slides/slide2.xml"},
		map[string]string{"ppt/slides/slide1.xml": slide(), "ppt/slides/slide2.xml": slide("Итоги")})
	if text, err = extract(t, material.TypePPTX, data); err != nil || text.String() != "[Слайд 2]\nИтоги" {
		t.Fatalf("text = %q, %v", text.String(), err)
	}
}

func TestTXT(t *testing.T) {
	utf16 := []byte{0xff, 0xfe}
	for _, r := range "Привет\r\nмир" {
		utf16 = append(utf16, byte(r), byte(r>>8))
	}
	tests := []struct {
		name string
		data string
		want string
	}{
		{"utf-8", "Строка 1\nСтрока 2\r\n\r\nСтрока 3", "[Фрагмент 1]\nСтрока 1\nСтрока 2\nСтрока 3"},
		{"utf-8 BOM", "\xef\xbb\xbfТекст", "[Фрагмент 1]\nТекст"},
		{"utf-16", string(utf16), "[Фрагмент 1]\nПривет\nмир"},
		{"form feeds", "раз\fдва\n\f\fчетыре", "[Страница 1]\nраз\n[Страница 2]\nдва\n[Страница 4]\nчетыре"},
	}
	for _, tt := range tests {
		text, err := extract(t, material.TypeTXT, []byte(tt.data))
		if err != nil || text.String() != tt.want {
			t.Errorf("%s: text = %q, %v; want %q", tt.name, text.String(), err, tt.want)
		}
	}
	if _, err := extract(t, material.TypeTXT, []byte("MZ\x00\x01binary")); !errors.Is(err, material.ErrTextUnreadable) {
		t.Fatalf("binary = %v", err)
	}
}

func TestTXTCutAtTheLimit(t *testing.T) {
	data := bytes.Repeat([]byte("я"), maxTXTBytes/2+1) // the cut falls inside a character
	text, err := extract(t, material.TypeTXT, data)
	if err != nil || !text.Truncated() {
		t.Fatalf("large text: %v, truncated %v", err, text.Truncated())
	}
	be := append([]byte{0xfe, 0xff}, bytes.Repeat([]byte{0xd8, 0x3d, 0xde, 0x00}, maxTXTBytes/4+1)...) // surrogate pairs
	if text, err = extract(t, material.TypeTXT, be); err != nil || !text.Truncated() {
		t.Fatalf("large utf-16: %v", err)
	}
	for _, tc := range [][]byte{[]byte("ab\xd0"), []byte("a\xe2\x82"), []byte("abc")} {
		if got := cutText(append(tc, 'x'), len(tc)); !bytes.Equal(got, bytes.TrimRight(tc, "\xd0\xe2\x82")) {
			t.Errorf("cutText(%q) = %q", tc, got)
		}
	}
}

func TestPDFIsNotExtracted(t *testing.T) {
	if _, err := extract(t, material.TypePDF, []byte("%PDF-1.7")); !errors.Is(err, material.ErrTextNotExtracted) {
		t.Fatalf("pdf = %v", err)
	}
}

func TestUnreadable(t *testing.T) {
	manyEntries := make([]entry, maxEntries+1)
	for i := range manyEntries {
		manyEntries[i] = entry{fmt.Sprintf("e%d", i), ""}
	}
	tests := []struct {
		name string
		typ  material.Type
		data []byte
	}{
		{"not a zip", material.TypeDOCX, []byte("PK\x03\x04garbage")},
		{"no document part", material.TypeDOCX, zipOf(t, entry{"word/other.xml", "<x/>"})},
		{"broken xml", material.TypeDOCX, zipOf(t, entry{docxBody, "<w:document><w:p>"})},
		{"undefined entity", material.TypeDOCX, zipOf(t, entry{docxBody, `<!DOCTYPE d [<!ENTITY a "aaaa">]><d>&a;</d>`})},
		{"too many entries", material.TypeDOCX, zipOf(t, append(manyEntries, entry{docxBody, "<d/>"})...)},
		{"duplicate entry", material.TypeDOCX, zipOf(t, entry{docxBody, "<d/>"}, entry{docxBody, "<d/>"})},
		{"no presentation part", material.TypePPTX, zipOf(t, entry{pptxMainRels, fmt.Sprintf(relsXML, "")})},
		{"slide outside ppt", material.TypePPTX, pptxOf(t, []string{"rId1"}, map[string]string{"rId1": "../../evil.xml"}, nil)},
		{"slide not xml", material.TypePPTX, pptxOf(t, []string{"rId1"}, map[string]string{"rId1": "media/a.png"}, nil)},
		{"unknown relationship", material.TypePPTX, pptxOf(t, []string{"rId9"}, map[string]string{}, nil)},
		{"missing slide part", material.TypePPTX, pptxOf(t, []string{"rId1"}, map[string]string{"rId1": "slides/slide1.xml"}, nil)},
	}
	for _, tt := range tests {
		if _, err := extract(t, tt.typ, tt.data); !errors.Is(err, material.ErrTextUnreadable) {
			t.Errorf("%s: %v, want ErrTextUnreadable", tt.name, err)
		}
	}
}

// A ZIP bomb: a part that unpacks far beyond the limit from a small archive stops at the limit.
func TestZipBomb(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(docxBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "<w:document "+wordNS+"><w:body>"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(w, io.LimitReader(zeroSpaces{}, maxPartBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > maxPartBytes/100 {
		t.Fatalf("the bomb is %d bytes packed", buf.Len())
	}
	if _, err := extract(t, material.TypeDOCX, buf.Bytes()); !errors.Is(err, material.ErrTextUnreadable) {
		t.Fatalf("zip bomb = %v", err)
	}
}

type zeroSpaces struct{}

func (zeroSpaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

// A huge text inside the limits of the part is cut at material.MaxTextBytes and marked truncated.
func TestHugeTextIsTruncated(t *testing.T) {
	big := strings.Repeat("слово ", material.MaxTextBytes/len("слово ")+100)
	text, err := extract(t, material.TypeDOCX, docxOf(t, para("начало")+para(big)+para("конец")))
	if err != nil || !text.Truncated() || strings.Contains(text.String(), "конец") {
		t.Fatalf("huge text: %v, truncated %v", err, text.Truncated())
	}
}

func TestContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Extract(ctx, material.TypeTXT, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract = %v", err)
	}
}
