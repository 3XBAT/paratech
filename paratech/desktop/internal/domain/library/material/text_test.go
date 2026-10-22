package material

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewText(t *testing.T) {
	text, err := NewText([]Fragment{
		{Locator: Locator{Kind: LocatorSlide, N: 1}, Lines: []string{" Введение ", "", "a\tb\x00c"}},
		{Locator: Locator{Kind: LocatorSlide, N: 2}, Lines: []string{"   "}}, // no line left: dropped
		{Locator: Locator{Kind: LocatorSlide, N: 4}, Lines: []string{"Итоги"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "[Слайд 1]\nВведение\na b c\n[Слайд 4]\nИтоги"
	if got := text.String(); got != want || text.Empty() || text.Truncated() {
		t.Fatalf("String = %q, want %q", got, want)
	}
	if len(text.Fragments()) != 2 {
		t.Fatalf("fragments = %+v", text.Fragments())
	}

	bad := map[string][]Fragment{
		"zero number":    {{Locator: Locator{Kind: LocatorPage, N: 0}, Lines: []string{"x"}}},
		"not rising":     {{Locator: Locator{Kind: LocatorPage, N: 2}}, {Locator: Locator{Kind: LocatorPage, N: 2}}},
		"mixed kinds":    {{Locator: Locator{Kind: LocatorPage, N: 1}}, {Locator: Locator{Kind: LocatorSlide, N: 2}}},
		"unknown kind":   {{Locator: Locator{Kind: "chapter", N: 1}}},
		"falling number": {{Locator: Locator{Kind: LocatorFragment, N: 3}}, {Locator: Locator{Kind: LocatorFragment, N: 1}}},
	}
	for name, fs := range bad {
		if _, err := NewText(fs, false); !errors.Is(err, ErrInvalidText) {
			t.Errorf("%s: NewText = %v", name, err)
		}
	}

	empty, err := NewText(nil, false)
	if err != nil || !empty.Empty() || empty.String() != "" {
		t.Fatalf("empty text = %v, %q", err, empty.String())
	}
}

func TestNewTextCutsAtTheLimits(t *testing.T) {
	line := strings.Repeat("я", 1000) // 2000 bytes
	var fs []Fragment
	for i := range MaxTextBytes/2000 + 10 {
		fs = append(fs, Fragment{Locator: Locator{Kind: LocatorPage, N: i + 1}, Lines: []string{line}})
	}
	text, err := NewText(fs, false)
	if err != nil {
		t.Fatal(err)
	}
	if !text.Truncated() || len(text.String()) > MaxTextBytes {
		t.Fatalf("truncated = %v, %d bytes", text.Truncated(), len(text.String()))
	}

	fs = nil
	for i := range MaxFragments + 1 {
		fs = append(fs, Fragment{Locator: Locator{Kind: LocatorSlide, N: i + 1}, Lines: []string{"x"}})
	}
	if text, err = NewText(fs, false); err != nil || !text.Truncated() || len(text.Fragments()) != MaxFragments {
		t.Fatalf("over MaxFragments: %v, truncated %v, %d", err, text.Truncated(), len(text.Fragments()))
	}

	if text, _ = NewText(nil, true); !text.Truncated() {
		t.Fatal("the reader's truncation is kept")
	}
}

func TestFragmentsOf(t *testing.T) {
	long := strings.Repeat("a", FragmentRunes-10)
	fs := FragmentsOf([]string{long, "bbbbbbbbbbbbbbbbbbbb", "c", strings.Repeat("d", FragmentRunes*2)})
	if len(fs) != 3 {
		t.Fatalf("fragments = %d", len(fs))
	}
	for i, f := range fs {
		if f.Locator != (Locator{Kind: LocatorFragment, N: i + 1}) {
			t.Fatalf("locator %d = %+v", i, f.Locator)
		}
	}
	if len(fs[1].Lines) != 2 || len(fs[2].Lines) != 1 {
		t.Fatalf("lines = %d, %d", len(fs[1].Lines), len(fs[2].Lines))
	}
	if FragmentsOf(nil) != nil {
		t.Fatal("no lines, no fragments")
	}
}

func TestTextHeadAndDigest(t *testing.T) {
	text, err := NewText([]Fragment{{Locator: Locator{Kind: LocatorPage, N: 1}, Lines: []string{"первая строка", "вторая"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := text.Head(1000); got != text.String() {
		t.Fatalf("Head(large) = %q", got)
	}
	if got := text.Head(len("[Страница 1]\nпервая строка") + 3); got != "[Страница 1]\nпервая строка" {
		t.Fatalf("Head at a line end = %q", got)
	}
	if got := text.Head(len("[Страниц") + 1); !utf8.ValidString(got) || got != "[Страниц" {
		t.Fatalf("Head inside the first line = %q", got)
	}

	cut, _ := NewText(text.Fragments(), true)
	if text.Digest() == cut.Digest() || text.Digest().Validate() != nil {
		t.Fatal("a truncated text has another digest")
	}
	same, _ := NewText(text.Fragments(), false)
	if same.Digest() != text.Digest() {
		t.Fatal("the same text has the same digest")
	}
}

func TestDecodeText(t *testing.T) {
	utf16le := []byte{0xff, 0xfe, 0x1f, 0x04, 0x40, 0x04, '\n', 0} // «Пр\n»
	utf16be := []byte{0xfe, 0xff, 0x04, 0x1f, 0x04, 0x40}
	tests := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{"utf-8", []byte("Привет"), "Привет", true},
		{"utf-8 with BOM", []byte("\xef\xbb\xbfПривет"), "Привет", true},
		{"utf-16 le", utf16le, "Пр\n", true},
		{"utf-16 be", utf16be, "Пр", true},
		{"utf-16 odd length", append(utf16be, 0x04), "", false},
		{"utf-16 unpaired surrogate", []byte{0xff, 0xfe, 0x00, 0xd8}, "", false},
		{"utf-16 with NUL", []byte{0xff, 0xfe, 0x00, 0x00}, "", false},
		{"only a BOM", []byte("\xef\xbb\xbf"), "", false},
		{"binary", []byte("MZ\x00\x00"), "", false},
		{"cp1251", []byte{0xcf, 0xf0, 0xe8}, "", false},
	}
	for _, tt := range tests {
		got, err := DecodeText(tt.data)
		if got != tt.want || (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrUnsupportedType)) {
			t.Errorf("%s: DecodeText = %q, %v", tt.name, got, err)
		}
	}
	if typ, err := DetectType(utf16le); typ != TypeTXT || err != nil {
		t.Fatalf("DetectType(utf-16) = %q, %v", typ, err)
	}
}

// SetText records the reading of the text; a PDF keeps its own status and a removed material refuses.
func TestSetText(t *testing.T) {
	m := newMaterial(t, TypeDOCX)
	if changed, err := m.SetText(TextExtracted, t0); err != nil || !changed || m.TextStatus() != TextExtracted {
		t.Fatalf("SetText = %v, %v", changed, err)
	}
	if changed, err := m.SetText(TextExtracted, t0); err != nil || changed {
		t.Fatalf("SetText again = %v, %v", changed, err)
	}
	if changed, err := m.SetText(TextFailed, t0); err != nil || !changed {
		t.Fatalf("SetText(failed) = %v, %v", changed, err)
	}
	for _, s := range []TextStatus{TextPending, TextPDFNotExtracted, "other"} {
		if _, err := m.SetText(s, t0); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("SetText(%q) = %v", s, err)
		}
	}
	if _, err := newMaterial(t, TypePDF).SetText(TextExtracted, t0); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("SetText of a PDF = %v", err)
	}
	if _, err := m.Remove(t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetText(TextExtracted, t0); !errors.Is(err, ErrRemoved) {
		t.Fatalf("SetText of a removed material = %v", err)
	}
}
