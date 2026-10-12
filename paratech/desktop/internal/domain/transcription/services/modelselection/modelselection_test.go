package modelselection

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

func model(t *testing.T, id string) *sttmodel.Model {
	t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, id+".bin")
	if err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: shared.SttModelID(id), Name: values.ModelName(id), ModelVersion: "1", File: file, SizeBytes: 1,
		SHA256: values.SHA256(fmt.Sprintf("%064x", len(id))),
	}, time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestResolve(t *testing.T) {
	a, b := model(t, "a"), model(t, "bb")
	cases := []struct {
		name      string
		requested shared.SttModelID
		def       shared.SttModelID
		installed []*sttmodel.Model
		want      shared.SttModelID
		err       error
	}{
		{"requested wins over the default", "bb", "a", []*sttmodel.Model{a, b}, "bb", nil},
		{"default when nothing requested", "", "a", []*sttmodel.Model{a, b}, "a", nil},
		{"the only model without a default", "", "", []*sttmodel.Model{b}, "bb", nil},
		{"requested but not installed", "zz", "a", []*sttmodel.Model{a, b}, "", ErrModelNotInstalled},
		{"default but not installed", "", "zz", []*sttmodel.Model{a, b}, "", ErrModelNotInstalled},
		{"nothing installed", "", "", nil, "", ErrModelNotInstalled},
		{"several models, no default", "", "", []*sttmodel.Model{a, b}, "", ErrNoDefaultModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New().Resolve(tc.requested, tc.def, tc.installed)
			if tc.err != nil {
				if !errors.Is(err, tc.err) || got != nil {
					t.Fatalf("Resolve = %v, %v; want %v", got, err, tc.err)
				}
				return
			}
			if err != nil || got.ID() != tc.want {
				t.Fatalf("Resolve = %v, %v; want %s", got, err, tc.want)
			}
		})
	}
	if !errors.Is(ErrModelNotInstalled, shared.ErrNotFound) {
		t.Fatal("ErrModelNotInstalled must wrap shared.ErrNotFound")
	}
}

func TestDefaultAfterRemoval(t *testing.T) {
	cases := []struct {
		current, removed, want shared.SttModelID
		changed                bool
	}{
		{"a", "a", "", true},
		{"a", "b", "a", false},
		{"", "a", "", false},
	}
	for _, tc := range cases {
		got, changed := New().DefaultAfterRemoval(tc.current, tc.removed)
		if got != tc.want || changed != tc.changed {
			t.Errorf("DefaultAfterRemoval(%q, %q) = %q, %v; want %q, %v", tc.current, tc.removed, got, changed, tc.want, tc.changed)
		}
	}
}

func entry(t *testing.T, id string, cores int, ramMB, size int64, preferred bool) values.CatalogEntry {
	t.Helper()
	src, err := values.ParseSourceURL("https://example.com/" + id + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	e, err := values.NewCatalogEntry(values.CatalogEntrySpec{
		ID: values.CatalogID(id), Name: values.ModelName(id), Version: "1", SizeBytes: size,
		SHA256: values.SHA256(fmt.Sprintf("%064x", size)), SourceURL: src,
		Recommendation: values.Recommendation{MinCPUCores: cores, MinRAMMB: ramMB, Preferred: preferred},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func machine(t *testing.T, cores int, ramMB int64) values.Machine {
	t.Helper()
	m, err := values.NewMachine(cores, ramMB)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRecommend(t *testing.T) {
	tiny := entry(t, "tiny", 1, 512, 10, true)
	base := entry(t, "base", 2, 1024, 20, true)
	small := entry(t, "small", 4, 2048, 30, true)
	large := entry(t, "large", 8, 8192, 40, false) // opt-in, never recommended
	baseTwin := entry(t, "base-twin", 2, 1024, 20+1, true)
	all := []values.CatalogEntry{large, small, tiny, base}

	cases := []struct {
		name    string
		entries []values.CatalogEntry
		m       values.Machine
		want    values.CatalogID
		ok      bool
	}{
		{"unknown machine gets the lightest preferred", all, values.Machine{}, "tiny", true},
		{"strong machine gets the strongest preferred, not the opt-in", all, machine(t, 16, 32768), "small", true},
		{"middle machine", all, machine(t, 2, 1536), "base", true},
		{"cores limit", all, machine(t, 3, 32768), "base", true},
		{"weak machine gets the lightest preferred", all, machine(t, 1, 256), "tiny", true},
		{"tie on hardware is broken by size", []values.CatalogEntry{baseTwin, base}, machine(t, 2, 1024), "base-twin", true},
		{"no preferred entry", []values.CatalogEntry{large}, machine(t, 16, 32768), "", false},
		{"empty catalog", nil, values.Machine{}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := New().Recommend(tc.entries, tc.m)
			if ok != tc.ok || got.ID() != tc.want {
				t.Fatalf("Recommend = %q, %v; want %q, %v", got.ID(), ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLikelySlow(t *testing.T) {
	small := entry(t, "small", 4, 2048, 30, true)
	cases := []struct {
		name string
		m    values.Machine
		want bool
	}{
		{"unknown machine", values.Machine{}, false},
		{"meets", machine(t, 4, 2048), false},
		{"too few cores", machine(t, 2, 4096), true},
		{"too little memory", machine(t, 8, 1024), true},
	}
	for _, tc := range cases {
		if got := New().LikelySlow(small, tc.m); got != tc.want {
			t.Errorf("%s: LikelySlow = %v, want %v", tc.name, got, tc.want)
		}
	}
}
