package sttmodel

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

const sum = values.SHA256("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

func ref(t *testing.T, kind shared.ArtifactKind, path string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.NewArtifactRef(kind, path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func goodSpec(t *testing.T) Spec {
	return Spec{
		ID: "m1", Name: "base", ModelVersion: "1", CatalogID: "base-q5_1", File: ref(t, shared.ArtifactModels, "m1.bin"),
		SizeBytes: 100, SHA256: sum, Quantization: "q5_1", Capabilities: []values.Capability{"multilingual"},
	}
}

func TestNew(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Spec)
		at     time.Time
		ok     bool
	}{
		{"valid", func(*Spec) {}, t0, true},
		{"imported outside the catalog", func(s *Spec) { s.CatalogID = "" }, t0, true},
		{"no quantization or capabilities", func(s *Spec) { s.Quantization, s.Capabilities = "", nil }, t0, true},
		{"nested file", func(s *Spec) { s.File = ref(t, shared.ArtifactModels, "m1/ggml.bin") }, t0, true},
		{"no installation time", func(*Spec) {}, time.Time{}, false},
		{"bad id", func(s *Spec) { s.ID = "a/b" }, t0, false},
		{"empty id", func(s *Spec) { s.ID = "" }, t0, false},
		{"empty name", func(s *Spec) { s.Name = "" }, t0, false},
		{"bad name", func(s *Spec) { s.Name = "base model" }, t0, false},
		{"empty model version", func(s *Spec) { s.ModelVersion = "" }, t0, false},
		{"bad model version", func(s *Spec) { s.ModelVersion = "1/2" }, t0, false},
		{"bad catalog id", func(s *Spec) { s.CatalogID = "a b" }, t0, false},
		{"no file", func(s *Spec) { s.File = shared.ArtifactRef{} }, t0, false},
		{"file outside models", func(s *Spec) { s.File = ref(t, shared.ArtifactRecordings, "m1.bin") }, t0, false},
		{"zero size", func(s *Spec) { s.SizeBytes = 0 }, t0, false},
		{"negative size", func(s *Spec) { s.SizeBytes = -1 }, t0, false},
		{"huge size", func(s *Spec) { s.SizeBytes = values.MaxModelSizeBytes + 1 }, t0, false},
		{"no digest", func(s *Spec) { s.SHA256 = "" }, t0, false},
		{"uppercase digest", func(s *Spec) { s.SHA256 = values.SHA256(strings.ToUpper(string(sum))) }, t0, false},
		{"bad quantization", func(s *Spec) { s.Quantization = "Q5" }, t0, false},
		{"repeated capability", func(s *Spec) { s.Capabilities = []values.Capability{"en", "en"} }, t0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := goodSpec(t)
			tc.mutate(&spec)
			m, err := New(spec, tc.at)
			if !tc.ok {
				if !errors.Is(err, ErrInvalidModel) || m != nil {
					t.Fatalf("New = %v, %v; want ErrInvalidModel", m, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.Version() != 0 || m.ID() != spec.ID || m.Name() != spec.Name || m.ModelVersion() != spec.ModelVersion ||
				m.CatalogID() != spec.CatalogID || m.File() != spec.File || m.SizeBytes() != spec.SizeBytes ||
				m.SHA256() != spec.SHA256 || m.Quantization() != spec.Quantization || !m.InstalledAt().Equal(tc.at) ||
				len(m.Capabilities()) != len(spec.Capabilities) {
				t.Fatalf("model does not reflect its spec: %+v", m)
			}
		})
	}
}

func TestCapabilitiesAreCopied(t *testing.T) {
	spec := goodSpec(t)
	m, err := New(spec, t0)
	if err != nil {
		t.Fatal(err)
	}
	spec.Capabilities[0] = "changed"
	m.Capabilities()[0] = "changed"
	if m.Capabilities()[0] != "multilingual" {
		t.Fatalf("capabilities changed from outside: %v", m.Capabilities())
	}
}

func TestRestore(t *testing.T) {
	cases := []struct {
		name    string
		version int64
		mutate  func(*Spec)
		ok      bool
	}{
		{"stored", 3, func(*Spec) {}, true},
		{"version zero", 0, func(*Spec) {}, false},
		{"negative version", -1, func(*Spec) {}, false},
		{"corrupt digest", 1, func(s *Spec) { s.SHA256 = "x" }, false},
		{"corrupt file", 1, func(s *Spec) { s.File = ref(t, shared.ArtifactExports, "m1.bin") }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := goodSpec(t)
			tc.mutate(&spec)
			m, err := Restore(State{Spec: spec, Version: tc.version, InstalledAt: t0})
			if !tc.ok {
				if !errors.Is(err, ErrInvalidModel) {
					t.Fatalf("Restore error = %v, want ErrInvalidModel", err)
				}
				return
			}
			if err != nil || m.Version() != tc.version || m.Name() != "base" {
				t.Fatalf("Restore = %+v, %v", m, err)
			}
		})
	}
}

func TestCatalogSpec(t *testing.T) {
	src, err := values.ParseSourceURL("https://example.com/base.bin")
	if err != nil {
		t.Fatal(err)
	}
	e, err := values.NewCatalogEntry(values.CatalogEntrySpec{
		ID: "base-q5_1", Name: "base", Version: "1", SizeBytes: 42, SHA256: sum, SourceURL: src,
		Quantization: "q5_1", Capabilities: []values.Capability{"en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := ref(t, shared.ArtifactModels, "m9.bin")
	m, err := New(CatalogSpec("m9", e, file), t0)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "m9" || m.CatalogID() != "base-q5_1" || m.Name() != "base" || m.ModelVersion() != "1" ||
		m.SizeBytes() != 42 || m.SHA256() != sum || m.File() != file || m.Quantization() != "q5_1" ||
		len(m.Capabilities()) != 1 {
		t.Fatalf("model from the catalog = %+v", m)
	}
}

func TestCheckRemoval(t *testing.T) {
	m, err := New(goodSpec(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CheckRemoval(false); err != nil {
		t.Fatalf("unpinned model: %v", err)
	}
	err = m.CheckRemoval(true)
	if !errors.Is(err, ErrPinned) || !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("pinned model: %v, want ErrPinned and shared.ErrConflict", err)
	}
}

func TestCheckFileFree(t *testing.T) {
	newModel := func(id shared.SttModelID, name values.ModelName, path string) *Model {
		t.Helper()
		spec := goodSpec(t)
		spec.ID, spec.Name, spec.File = id, name, ref(t, shared.ArtifactModels, path)
		m, err := New(spec, t0)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := newModel("m1", "base", "dir/base.bin")

	cases := []struct {
		name  string
		other *Model
		want  error
	}{
		{"other file", newModel("m2", "tiny", "dir/tiny.bin"), nil},
		{"same file", newModel("m2", "tiny", "dir/base.bin"), ErrFileInUse},
		{"same file in other case", newModel("m2", "tiny", "DIR/Base.BIN"), ErrFileInUse},
		{"inside the other's directory", newModel("m2", "tiny", "dir"), ErrFileInUse},
		{"contains the other's file", newModel("m2", "tiny", "dir/base.bin/x.bin"), ErrFileInUse},
		{"name prefix only", newModel("m2", "tiny", "dir/base.bin2"), nil},
		{"the model itself", newModel("m1", "base", "dir/base.bin"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.CheckFileFree([]*Model{tc.other})
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("CheckFileFree = %v, want %v", err, tc.want)
			}
			if tc.want != nil && !errors.Is(err, shared.ErrDuplicate) {
				t.Fatalf("CheckFileFree = %v, want shared.ErrDuplicate", err)
			}
		})
	}
	if err := m.CheckFileFree(nil); err != nil {
		t.Fatalf("no other model: %v", err)
	}
}

func TestPersisted(t *testing.T) {
	m, err := New(goodSpec(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	m.Persisted(1)
	if m.Version() != 1 {
		t.Fatalf("Version() = %d after Persisted(1)", m.Version())
	}
}
