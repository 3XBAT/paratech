package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	sumA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sumB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// entryJSON returns one manifest entry; fields overrides or adds raw JSON members, "-" removes a member.
func entryJSON(fields map[string]string) string {
	members := []struct{ key, value string }{
		{"id", `"base-q5_1"`},
		{"name", `"base"`},
		{"version", `"1"`},
		{"size_bytes", `59707625`},
		{"sha256", `"` + sumA + `"`},
		{"source_url", `"https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base-q5_1.bin"`},
		{"quantization", `"q5_1"`},
		{"capabilities", `["multilingual"]`},
		{"recommendation", `{"min_cpu_cores": 2, "min_ram_mb": 1024, "preferred": true}`},
	}
	var parts []string
	seen := map[string]bool{}
	for _, m := range members {
		seen[m.key] = true
		v, ok := fields[m.key]
		switch {
		case !ok:
			v = m.value
		case v == "-":
			continue
		}
		parts = append(parts, fmt.Sprintf("%q: %s", m.key, v))
	}
	for k, v := range fields {
		if !seen[k] {
			parts = append(parts, fmt.Sprintf("%q: %s", k, v))
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func manifest(entries ...string) string {
	return `{"schema_version": 1, "models": [` + strings.Join(entries, ", ") + `]}`
}

func secondEntry() string {
	return entryJSON(map[string]string{
		"id": `"small-q5_1"`, "name": `"small"`, "sha256": `"` + sumB + `"`, "size_bytes": `190085487`,
		"source_url":     `"https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small-q5_1.bin"`,
		"recommendation": `{"min_cpu_cores": 4, "min_ram_mb": 2048, "preferred": true}`,
	})
}

func TestParseCatalogValid(t *testing.T) {
	c, err := ParseCatalog([]byte(manifest(entryJSON(nil), secondEntry())))
	if err != nil {
		t.Fatal(err)
	}
	entries := c.Entries()
	if len(entries) != 2 || entries[0].ID() != "base-q5_1" || entries[1].ID() != "small-q5_1" {
		t.Fatalf("entries = %+v", entries)
	}

	e := entries[0]
	switch {
	case e.Name() != "base" || e.Version() != "1" || e.SizeBytes() != 59707625 || e.SHA256() != sumA:
		t.Fatalf("identity = %s %s %d %s", e.Name(), e.Version(), e.SizeBytes(), e.SHA256())
	case e.SourceURL().Host() != "huggingface.co" || e.Quantization() != "q5_1":
		t.Fatalf("source %q quantization %q", e.SourceURL(), e.Quantization())
	case len(e.Capabilities()) != 1 || e.Capabilities()[0] != "multilingual":
		t.Fatalf("capabilities = %v", e.Capabilities())
	case e.Recommendation() != (Recommendation{MinCPUCores: 2, MinRAMMB: 1024, Preferred: true}):
		t.Fatalf("recommendation = %+v", e.Recommendation())
	}

	got, ok := c.Entry("small-q5_1")
	if !ok || got.Name() != "small" {
		t.Fatalf("Entry(small-q5_1) = %+v, %v", got, ok)
	}
	if _, ok := c.Entry("missing"); ok {
		t.Fatal("Entry(missing) found something")
	}
}

func TestParseCatalogOptionalFields(t *testing.T) {
	c, err := ParseCatalog([]byte(manifest(entryJSON(map[string]string{
		"quantization": "-", "capabilities": "-", "recommendation": "-", "sha256": `"` + strings.ToUpper(sumA) + `"`,
	}))))
	if err != nil {
		t.Fatal(err)
	}
	e := c.Entries()[0]
	if e.Quantization() != "" || len(e.Capabilities()) != 0 || e.Recommendation() != (Recommendation{}) || e.SHA256() != sumA {
		t.Fatalf("entry = %+v", e)
	}
}

func TestParseCatalogRejects(t *testing.T) {
	cases := []struct {
		name  string
		input string
		cause error // a narrower error the result must also wrap, if any
	}{
		{"empty input", ``, nil},
		{"whitespace only", `   `, nil},
		{"null", `null`, nil},
		{"array root", `[]`, nil},
		{"string root", `"x"`, nil},
		{"truncated", `{"schema_version": 1, "models": [`, nil},
		{"trailing object", manifest(entryJSON(nil)) + `{}`, nil},
		{"trailing garbage", manifest(entryJSON(nil)) + ` x`, nil},
		{"invalid utf-8", "{\"schema_version\": 1, \"models\": [], \"x\": \"\xff\"}", nil},
		{"no schema version", `{"models": [` + entryJSON(nil) + `]}`, nil},
		{"wrong schema version", `{"schema_version": 2, "models": [` + entryJSON(nil) + `]}`, nil},
		{"fractional schema version", `{"schema_version": 1.5, "models": [` + entryJSON(nil) + `]}`, nil},
		{"no models", `{"schema_version": 1}`, nil},
		{"null models", `{"schema_version": 1, "models": null}`, nil},
		{"no models with hosts", `{"schema_version": 1, "download_hosts": ["huggingface.co"]}`, nil},
		{"empty models, bad host", `{"schema_version": 1, "models": [], "download_hosts": ["bad host"]}`, ErrInvalidHost},
		{"empty models, wrong schema version", `{"schema_version": 2, "models": []}`, nil},
		{"models not an array", `{"schema_version": 1, "models": {}}`, nil},
		{"unknown top-level key", `{"schema_version": 1, "extra": 1, "models": [` + entryJSON(nil) + `]}`, nil},
		{"unknown entry key", manifest(entryJSON(map[string]string{"file_name": `"x.bin"`})), nil},
		{"unknown recommendation key", manifest(entryJSON(map[string]string{"recommendation": `{"gpu": true}`})), nil},
		{"repeated key", manifest(`{"sha256": "` + sumB + `", ` + entryJSON(nil)[1:]), nil},
		{"key differing in case", manifest(entryJSON(map[string]string{"SHA256": `"` + sumB + `"`})), nil},
		{"key with case folding rune", manifest(entryJSON(map[string]string{"ſha256": `"` + sumB + `"`})), nil},
		{"repeated top-level key", `{"schema_version": 1, "schema_version": 1, "models": [` + entryJSON(nil) + `]}`, nil},
		{"too deep", manifest(entryJSON(map[string]string{"capabilities": `[[[[[[[["x"]]]]]]]]`})), nil},
		{"missing id", manifest(entryJSON(map[string]string{"id": "-"})), ErrInvalidCatalogID},
		{"missing name", manifest(entryJSON(map[string]string{"name": "-"})), ErrInvalidModelName},
		{"missing version", manifest(entryJSON(map[string]string{"version": "-"})), ErrInvalidModelVersion},
		{"null name", manifest(entryJSON(map[string]string{"name": "null"})), ErrInvalidModelName},
		{"bad name", manifest(entryJSON(map[string]string{"name": `"../base"`})), ErrInvalidModelName},
		{"missing size", manifest(entryJSON(map[string]string{"size_bytes": "-"})), ErrInvalidSize},
		{"zero size", manifest(entryJSON(map[string]string{"size_bytes": "0"})), ErrInvalidSize},
		{"negative size", manifest(entryJSON(map[string]string{"size_bytes": "-5"})), ErrInvalidSize},
		{"huge size", manifest(entryJSON(map[string]string{"size_bytes": fmt.Sprint(MaxModelSizeBytes + 1)})), ErrInvalidSize},
		{"size overflow", manifest(entryJSON(map[string]string{"size_bytes": "99999999999999999999"})), nil},
		{"size as string", manifest(entryJSON(map[string]string{"size_bytes": `"10"`})), nil},
		{"missing sha256", manifest(entryJSON(map[string]string{"sha256": "-"})), ErrInvalidSHA256},
		{"short sha256", manifest(entryJSON(map[string]string{"sha256": `"abc"`})), ErrInvalidSHA256},
		{"missing source url", manifest(entryJSON(map[string]string{"source_url": "-"})), ErrInvalidSourceURL},
		{"http source url", manifest(entryJSON(map[string]string{"source_url": `"http://example.com/a.bin"`})), ErrInvalidSourceURL},
		{"credentials in url", manifest(entryJSON(map[string]string{"source_url": `"https://u:p@example.com/a.bin"`})), ErrInvalidSourceURL},
		{"token in query", manifest(entryJSON(map[string]string{"source_url": `"https://example.com/a.bin?token=x"`})), ErrInvalidSourceURL},
		{"bad quantization", manifest(entryJSON(map[string]string{"quantization": `"Q5"`})), ErrInvalidQuantization},
		{"bad capability", manifest(entryJSON(map[string]string{"capabilities": `["en", "en"]`})), ErrInvalidCapability},
		{"negative cores", manifest(entryJSON(map[string]string{"recommendation": `{"min_cpu_cores": -1}`})), nil},
		{"absurd ram", manifest(entryJSON(map[string]string{"recommendation": `{"min_ram_mb": 999999999}`})), nil},
		{"repeated id", manifest(entryJSON(nil), entryJSON(map[string]string{
			"name": `"other"`, "sha256": `"` + sumB + `"`})), nil},
		{"repeated name and version", manifest(entryJSON(nil), entryJSON(map[string]string{
			"id": `"other"`, "sha256": `"` + sumB + `"`})), nil},
		{"repeated sha256", manifest(entryJSON(nil), entryJSON(map[string]string{"id": `"other"`, "name": `"other"`})), nil},
		{"repeated sha256 in another case", manifest(entryJSON(nil), entryJSON(map[string]string{
			"id": `"other"`, "name": `"other"`, "sha256": `"` + strings.ToUpper(sumA) + `"`})), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseCatalog([]byte(tc.input))
			if !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("ParseCatalog = %+v, %v; want ErrInvalidCatalog", c, err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("error %v does not wrap %v", err, tc.cause)
			}
			if len(c.Entries()) != 0 {
				t.Fatalf("a rejected manifest produced entries: %+v", c.Entries())
			}
		})
	}
}

// TestParseCatalogEmpty: an explicit empty list is a catalog without downloadable models. Nothing can be looked up,
// imported names are free, and the download allowlist allows no host.
func TestParseCatalogEmpty(t *testing.T) {
	cases := []struct{ name, input string }{
		{"empty list", manifest()},
		{"empty list with spaces", `{"schema_version": 1, "models": [ ]}`},
		{"empty list with download hosts", `{"schema_version": 1, "models": [], "download_hosts": ["huggingface.co"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseCatalog([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if n := len(c.Entries()); n != 0 {
				t.Fatalf("%d entries, want 0", n)
			}
			if e, ok := c.Entry("base-q5_1"); ok {
				t.Fatalf("Entry found %+v", e)
			}
			if err := c.CheckImportIdentity("base", "1"); err != nil {
				t.Fatalf("CheckImportIdentity = %v", err)
			}
			if err := c.DownloadHosts().Check("https://example.com/a.bin"); !errors.Is(err, ErrHostNotAllowed) {
				t.Fatalf("Check(example.com) = %v, want ErrHostNotAllowed", err)
			}
		})
	}
	if c, _ := ParseCatalog([]byte(manifest())); len(c.DownloadHosts().Hosts()) != 0 {
		t.Fatalf("hosts of an empty catalog = %v, want none", c.DownloadHosts().Hosts())
	}
}

func TestParseCatalogBounds(t *testing.T) {
	big := []byte(manifest(entryJSON(nil)))
	big = append(big, []byte(strings.Repeat(" ", MaxCatalogBytes-len(big)))...)
	if _, err := ParseCatalog(big); err != nil {
		t.Fatalf("manifest of exactly MaxCatalogBytes: %v", err)
	}
	if _, err := ParseCatalog(append(big, ' ')); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("manifest over MaxCatalogBytes: %v", err)
	}

	entries := make([]string, MaxCatalogEntries+1)
	for i := range entries {
		entries[i] = entryJSON(map[string]string{
			"id": fmt.Sprintf(`"m%d"`, i), "name": fmt.Sprintf(`"m%d"`, i), "sha256": fmt.Sprintf(`"%064x"`, i+1),
		})
	}
	if c, err := ParseCatalog([]byte(manifest(entries[:MaxCatalogEntries]...))); err != nil || len(c.Entries()) != MaxCatalogEntries {
		t.Fatalf("MaxCatalogEntries entries: %d, %v", len(c.Entries()), err)
	}
	if _, err := ParseCatalog([]byte(manifest(entries...))); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("more than MaxCatalogEntries entries: %v", err)
	}
}

func TestCatalogIsImmutable(t *testing.T) {
	c, err := ParseCatalog([]byte(manifest(entryJSON(nil))))
	if err != nil {
		t.Fatal(err)
	}
	entries := c.Entries()
	entries[0] = CatalogEntry{}
	caps := c.Entries()[0].Capabilities()
	caps[0] = "changed"
	if e := c.Entries()[0]; e.ID() != "base-q5_1" || e.Capabilities()[0] != "multilingual" {
		t.Fatalf("catalog changed through a returned slice: %+v", e)
	}
}

func TestNewCatalogEntry(t *testing.T) {
	src, err := ParseSourceURL("https://example.com/a.bin")
	if err != nil {
		t.Fatal(err)
	}
	good := CatalogEntrySpec{ID: "a", Name: "a", Version: "1", SizeBytes: 10, SHA256: sumA, SourceURL: src}
	cases := []struct {
		name   string
		mutate func(*CatalogEntrySpec)
		cause  error
	}{
		{"valid", func(*CatalogEntrySpec) {}, nil},
		{"no source", func(s *CatalogEntrySpec) { s.SourceURL = SourceURL{} }, ErrInvalidSourceURL},
		{"non-canonical sha256", func(s *CatalogEntrySpec) { s.SHA256 = SHA256(strings.ToUpper(sumA)) }, ErrInvalidSHA256},
		{"bad id", func(s *CatalogEntrySpec) { s.ID = "a/b" }, ErrInvalidCatalogID},
		{"no size", func(s *CatalogEntrySpec) { s.SizeBytes = 0 }, ErrInvalidSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := good
			tc.mutate(&spec)
			_, err := NewCatalogEntry(spec)
			if tc.cause == nil && err != nil {
				t.Fatal(err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("error %v, want %v", err, tc.cause)
			}
		})
	}
}

func TestCatalogEntryVerify(t *testing.T) {
	c, err := ParseCatalog([]byte(manifest(entryJSON(nil))))
	if err != nil {
		t.Fatal(err)
	}
	e := c.Entries()[0]
	cases := []struct {
		name string
		sum  SHA256
		size int64
		want error
	}{
		{"match", sumA, 59707625, nil},
		{"other digest", sumB, 59707625, ErrChecksumMismatch},
		{"other size", sumA, 59707624, ErrSizeMismatch},
	}
	for _, tc := range cases {
		if err := e.Verify(tc.sum, tc.size); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%s: Verify = %v, want %v", tc.name, err, tc.want)
		}

		wantSize := tc.want
		if errors.Is(wantSize, ErrChecksumMismatch) {
			wantSize = nil
		}
		if err := e.VerifySize(tc.size); !errors.Is(err, wantSize) || (wantSize == nil && err != nil) {
			t.Errorf("%s: VerifySize = %v, want %v", tc.name, err, wantSize)
		}
	}
}

func TestCatalogCheckImportIdentity(t *testing.T) {
	c, err := ParseCatalog([]byte(manifest(entryJSON(nil), secondEntry())))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		model   ModelName
		version ModelVersion
		want    error
	}{
		{"first entry", "base", "1", ErrReservedIdentity},
		{"second entry", "small", "1", ErrReservedIdentity},
		{"catalog name, other version", "base", "2", nil},
		{"own name", "own", "1", nil},
		{"name differs in case", "Base", "1", nil},
	}
	for _, tc := range cases {
		err := c.CheckImportIdentity(tc.model, tc.version)
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%s: CheckImportIdentity = %v, want %v", tc.name, err, tc.want)
		}
	}
}
