package values

import (
	"errors"
	"testing"
)

func TestModelFamilyValidate(t *testing.T) {
	for _, f := range []ModelFamily{
		FamilyNone, FamilyTiny, FamilyBase, FamilySmall, FamilyMedium, FamilyLargeV3Turbo, FamilyLargeV3,
	} {
		if err := f.Validate(); err != nil {
			t.Errorf("%q: %v", f, err)
		}
	}
	for _, f := range []ModelFamily{"Small", "LARGE-V3", "large", "large-v2", "small.en", "small ", "-", "q5_1"} {
		if err := f.Validate(); !errors.Is(err, ErrInvalidModelFamily) {
			t.Errorf("%q: %v, want ErrInvalidModelFamily", f, err)
		}
	}
}

// The load rule of docs/refs/whisper.md: the family level for the full variant, one step lower for a quantized one,
// never below 1.
func TestLoadOf(t *testing.T) {
	cases := []struct {
		family ModelFamily
		quant  Quantization
		want   ModelLoad
	}{
		{FamilyTiny, "f16", 1},
		{FamilyTiny, "", 1},
		{FamilyTiny, "q5_1", 1}, // already the minimum
		{FamilyTiny, "q8_0", 1},
		{FamilyBase, "f16", 2},
		{FamilyBase, "q5_1", 1},
		{FamilySmall, "f16", 3},
		{FamilySmall, "q5_1", 2},
		{FamilySmall, "q8_0", 2},
		{FamilyMedium, "f16", 4},
		{FamilyMedium, "q5_0", 3},
		{FamilyLargeV3Turbo, "f16", 5},
		{FamilyLargeV3Turbo, "q5_0", 4},
		{FamilyLargeV3, "f16", 5},
		{FamilyLargeV3, "", 5},
		{FamilyLargeV3, "q5_0", 4},
		{FamilyLargeV3, "f32", 5}, // not quantized
		{FamilyNone, "f16", LoadUnknown},
		{FamilyNone, "q5_1", LoadUnknown},
		{"large-v2", "f16", LoadUnknown},
		{"Small", "q5_1", LoadUnknown},
	}
	for _, tc := range cases {
		if got := LoadOf(tc.family, tc.quant); got != tc.want {
			t.Errorf("LoadOf(%q, %q) = %d, want %d", tc.family, tc.quant, got, tc.want)
		}
	}
}

func TestModelLoadHeavy(t *testing.T) {
	for load, want := range map[ModelLoad]bool{LoadUnknown: false, 1: false, 2: false, 3: false, 4: true, 5: true} {
		if got := load.Heavy(); got != want {
			t.Errorf("ModelLoad(%d).Heavy() = %v, want %v", load, got, want)
		}
	}
}

func TestNewCatalogEntryFamily(t *testing.T) {
	src, err := ParseSourceURL("https://example.com/a.bin")
	if err != nil {
		t.Fatal(err)
	}
	spec := CatalogEntrySpec{
		ID: "a", Name: "a", Version: "1", SizeBytes: 10, SHA256: sumA, SourceURL: src, Quantization: "q5_0",
		Family: FamilyMedium,
	}
	e, err := NewCatalogEntry(spec)
	if err != nil {
		t.Fatal(err)
	}
	if e.Family() != FamilyMedium || e.Load() != 3 {
		t.Fatalf("family %q load %d, want medium 3", e.Family(), e.Load())
	}

	spec.Family = "huge"
	if _, err := NewCatalogEntry(spec); !errors.Is(err, ErrInvalidModelFamily) {
		t.Fatalf("NewCatalogEntry with family huge = %v, want ErrInvalidModelFamily", err)
	}
}

func TestParseCatalogFamily(t *testing.T) {
	cases := []struct {
		name     string
		field    string // raw JSON of "family", "-" leaves it out
		quant    string // raw JSON of "quantization"
		want     ModelFamily
		wantLoad ModelLoad
		bad      bool
	}{
		{"missing", "-", `"q5_1"`, FamilyNone, LoadUnknown, false},
		{"empty", `""`, `"q5_1"`, FamilyNone, LoadUnknown, false},
		{"null", `null`, `"q5_1"`, FamilyNone, LoadUnknown, false},
		{"tiny quantized", `"tiny"`, `"q5_1"`, FamilyTiny, 1, false},
		{"base full", `"base"`, `"f16"`, FamilyBase, 2, false},
		{"small quantized", `"small"`, `"q5_1"`, FamilySmall, 2, false},
		{"medium full", `"medium"`, `"f16"`, FamilyMedium, 4, false},
		{"turbo quantized", `"large-v3-turbo"`, `"q5_0"`, FamilyLargeV3Turbo, 4, false},
		{"large-v3 full", `"large-v3"`, `"f16"`, FamilyLargeV3, 5, false},
		{"unknown value", `"large"`, `"q5_1"`, "", 0, true},
		{"wrong case", `"Small"`, `"q5_1"`, "", 0, true},
		{"english suffix", `"small.en"`, `"q5_1"`, "", 0, true},
		{"number", `3`, `"q5_1"`, "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]string{"quantization": tc.quant}
			if tc.field != "-" {
				fields["family"] = tc.field
			}
			c, err := ParseCatalog([]byte(manifest(entryJSON(fields))))
			if tc.bad {
				if !errors.Is(err, ErrInvalidCatalog) {
					t.Fatalf("ParseCatalog = %v, want ErrInvalidCatalog", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			e := c.Entries()[0]
			if e.Family() != tc.want || e.Load() != tc.wantLoad {
				t.Fatalf("family %q load %d, want %q %d", e.Family(), e.Load(), tc.want, tc.wantLoad)
			}
		})
	}

	_, err := ParseCatalog([]byte(manifest(entryJSON(map[string]string{"family": `"huge"`}))))
	if !errors.Is(err, ErrInvalidModelFamily) {
		t.Fatalf("unknown family: %v, want ErrInvalidModelFamily", err)
	}
	// The strict shape still applies to the new key.
	_, err = ParseCatalog([]byte(manifest(entryJSON(map[string]string{"Family": `"base"`}))))
	if !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("key Family: %v, want ErrInvalidCatalog", err)
	}
}
