package values

import (
	"errors"
	"testing"
)

func TestCatalogEntrySourcePage(t *testing.T) {
	const fileURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base-q5_1.bin"
	cases := []struct {
		name   string
		fields map[string]string
		want   string
	}{
		{"no source page: the source url", nil, fileURL},
		{"null source page: the source url", map[string]string{"source_page": "null"}, fileURL},
		{"own source page", map[string]string{"source_page": `"https://HuggingFace.co/ggerganov/whisper.cpp"`},
			"https://huggingface.co/ggerganov/whisper.cpp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseCatalog([]byte(manifest(entryJSON(tc.fields))))
			if err != nil {
				t.Fatal(err)
			}
			e := c.Entries()[0]
			if got := e.SourcePage().String(); got != tc.want {
				t.Fatalf("SourcePage = %q, want %q", got, tc.want)
			}
			if e.SourceURL().String() != fileURL {
				t.Fatalf("SourceURL = %q, the page must not replace it", e.SourceURL())
			}
		})
	}
}

// TestCatalogSourcePageNotAllowedForDownload: the page is for the browser, its host never joins the allowlist.
func TestCatalogSourcePageNotAllowedForDownload(t *testing.T) {
	c, err := ParseCatalog([]byte(manifest(entryJSON(map[string]string{"source_page": `"https://pages.example.com/base"`}))))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DownloadHosts().Check("https://pages.example.com/base.bin"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("Check(page host) = %v, want ErrHostNotAllowed", err)
	}
}

func TestParseCatalogRejectsBadSourcePage(t *testing.T) {
	for name, page := range map[string]string{
		"empty":        `""`,
		"http":         `"http://example.com/page"`,
		"javascript":   `"javascript:alert(1)"`,
		"file":         `"file:///C:/Windows/System32/calc.exe"`,
		"credentials":  `"https://u:p@example.com/page"`,
		"query":        `"https://example.com/page?token=x"`,
		"fragment":     `"https://example.com/page#x"`,
		"root path":    `"https://example.com/"`,
		"not a string": `5`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCatalog([]byte(manifest(entryJSON(map[string]string{"source_page": page}))))
			if !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("ParseCatalog = %v, want ErrInvalidCatalog", err)
			}
		})
	}
}

func TestCheckImportSize(t *testing.T) {
	cases := []struct {
		size int64
		want error
	}{
		{1, nil},
		{MaxImportSizeBytes, nil},
		{0, ErrInvalidSize},
		{-1, ErrInvalidSize},
		{MaxImportSizeBytes + 1, ErrImportTooLarge},
		{MaxModelSizeBytes + 1, ErrInvalidSize},
	}
	for _, tc := range cases {
		err := CheckImportSize(tc.size)
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("CheckImportSize(%d) = %v, want %v", tc.size, err, tc.want)
		}
	}
	if err := CheckImportSize(MaxImportSizeBytes + 1); !errors.Is(err, ErrInvalidSize) {
		t.Errorf("ErrImportTooLarge must also be ErrInvalidSize: %v", err)
	}
}

func TestCheckImportSpace(t *testing.T) {
	const size = 100
	if err := CheckImportSpace(size, size+DiskSpaceMargin); err != nil {
		t.Fatalf("exactly enough: %v", err)
	}
	err := CheckImportSpace(size, size+DiskSpaceMargin-1)
	var ise *InsufficientSpaceError
	if !errors.As(err, &ise) || !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("one byte short: %v, want *InsufficientSpaceError", err)
	}
	if ise.Required != size+DiskSpaceMargin || ise.Available != size+DiskSpaceMargin-1 {
		t.Fatalf("numbers = %+v", ise)
	}
}

func TestImportConfirmation(t *testing.T) {
	confirmed, err := ConfirmImport(sumA)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.IsZero() || confirmed.SHA256() != sumA {
		t.Fatalf("ConfirmImport = %+v", confirmed)
	}

	cases := []struct {
		name   string
		c      ImportConfirmation
		actual SHA256
		want   error
	}{
		{"confirmed file", confirmed, sumA, nil},
		{"not confirmed", ImportConfirmation{}, sumA, ErrConfirmationRequired},
		{"another file", confirmed, sumB, ErrConfirmationMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Verify(tc.actual)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("Verify = %v, want %v", err, tc.want)
			}
		})
	}

	if err := confirmed.Verify(sumB); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("a mismatch must also be ErrChecksumMismatch: %v", err)
	}
	if err := (ImportConfirmation{}).Require(); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("Require of the zero value = %v", err)
	}
	if err := confirmed.Require(); err != nil {
		t.Fatalf("Require = %v", err)
	}
	for _, bad := range []SHA256{"", "abc", SHA256("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")} {
		if _, err := ConfirmImport(bad); !errors.Is(err, ErrInvalidSHA256) {
			t.Errorf("ConfirmImport(%q) = %v, want ErrInvalidSHA256", bad, err)
		}
	}
}
