package app

import (
	"context"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

// The downloads and their pages are pinned to one ggerganov/whisper.cpp commit (docs/refs/whisper.md).
const (
	catalogSourcePrefix = "https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/ggml-"
	catalogPagePrefix   = "https://huggingface.co/ggerganov/whisper.cpp/blob/5359861c739e955e79d9a303bcbc70fb988958b1/ggml-"
)

// embeddedEntries parses the embedded manifest the way the model manager does.
func embeddedEntries(t *testing.T) values.Catalog {
	t.Helper()
	data, err := (embeddedCatalog{}).Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cat, err := values.ParseCatalog(data)
	if err != nil {
		t.Fatalf("ParseCatalog = %v", err)
	}
	return cat
}

// TASK-067: the embedded catalog lists the recommended models of the official repository, pinned to one commit, with
// verified digests; small is the one preferred model.
func TestEmbeddedCatalog(t *testing.T) {
	cat := embeddedEntries(t)
	want := []struct {
		id    values.CatalogID
		tier  values.ModelTier
		cap   values.Capability
		quant values.Quantization
		size  int64
		sum   values.SHA256 // lfs.oid of the Hugging Face API, 2026-10-16
	}{
		{"small-q5_1", values.TierBalanced, values.CapabilityMultilingual, "q5_1", 190085487,
			"ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb"},
		{"base-q5_1", values.TierFast, values.CapabilityMultilingual, "q5_1", 59707625,
			"422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898"},
		{"medium-q5_0", values.TierAccurate, values.CapabilityMultilingual, "q5_0", 539212467,
			"19fea4b380c3a618ec4723c3eef2eb785ffba0d0538cf43f8f235e7b3b34220f"},
		{"large-v3-turbo-q5_0", values.TierMax, values.CapabilityMultilingual, "q5_0", 574041195,
			"394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2"},
		{"base.en-q5_1", values.TierFast, values.CapabilityEnglish, "q5_1", 59721011,
			"4baf70dd0d7c4247ba2b81fafd9c01005ac77c2f9ef064e00dcf195d0e2fdd2f"},
		{"small.en-q5_1", values.TierBalanced, values.CapabilityEnglish, "q5_1", 190098681,
			"bfdff4894dcb76bbf647d56263ea2a96645423f1669176f4844a1bf8e478ad30"},
	}
	entries := cat.Entries()
	if len(entries) != len(want) {
		t.Fatalf("%d catalog entries, want %d", len(entries), len(want))
	}

	preferred := 0
	for i, w := range want {
		e := entries[i]
		src := e.SourceURL().String()
		switch {
		case e.ID() != w.id || string(e.Name()) != string(w.id) || e.Version() != "1":
			t.Errorf("entry %d = %s %s %s, want %s", i, e.ID(), e.Name(), e.Version(), w.id)
		case src != catalogSourcePrefix+string(w.id)+".bin":
			t.Errorf("%s: source %s, want %s<id>.bin", w.id, src, catalogSourcePrefix)
		case e.SourcePage().String() != catalogPagePrefix+string(w.id)+".bin":
			t.Errorf("%s: source page %s", w.id, e.SourcePage())
		case e.SHA256() != w.sum || e.SizeBytes() != w.size:
			t.Errorf("%s: sha256 %s size %d, want %s %d", w.id, e.SHA256(), e.SizeBytes(), w.sum, w.size)
		case e.Tier() != w.tier || e.Quantization() != w.quant:
			t.Errorf("%s: tier %q quantization %q, want %q %q", w.id, e.Tier(), e.Quantization(), w.tier, w.quant)
		case !slices.Equal(e.Capabilities(), []values.Capability{w.cap}):
			t.Errorf("%s: capabilities %v, want %v", w.id, e.Capabilities(), w.cap)
		case e.EnglishOnly() != (w.cap == values.CapabilityEnglish):
			t.Errorf("%s: EnglishOnly = %v", w.id, e.EnglishOnly())
		case e.Recommendation().MinCPUCores <= 0 || e.Recommendation().MinRAMMB <= 0:
			t.Errorf("%s: recommendation %+v", w.id, e.Recommendation())
		}
		if e.Recommendation().Preferred {
			preferred++
			if w.id != "small-q5_1" {
				t.Errorf("%s is preferred, want only small-q5_1", w.id)
			}
		}
	}
	if preferred != 1 {
		t.Errorf("%d preferred entries, want 1", preferred)
	}

	// The slowest model asks for the most hardware, so the page can warn on a weaker machine.
	turbo, _ := cat.Entry("large-v3-turbo-q5_0")
	for _, e := range entries {
		if e.ID() != turbo.ID() && e.Recommendation().MinCPUCores >= turbo.Recommendation().MinCPUCores {
			t.Errorf("%s needs %d cores, the turbo model %d", e.ID(), e.Recommendation().MinCPUCores, turbo.Recommendation().MinCPUCores)
		}
	}
}

// Hugging Face redirects a download to its CDN: the allowlist has the source host and the CDN hosts, and nothing that
// resolves to a private address (cas-bridge-direct.xethub.hf.co).
func TestEmbeddedCatalogDownloadHosts(t *testing.T) {
	hosts := embeddedEntries(t).DownloadHosts()
	for _, h := range []string{
		"huggingface.co", "us.aws.cdn.hf.co", "cas-bridge.xethub.hf.co", "transfer.xethub.hf.co", "cdn-lfs.hf.co",
		"cdn-lfs-us-1.hf.co", "cdn-lfs-eu-1.hf.co",
	} {
		if err := hosts.Check("https://" + h + "/file.bin"); err != nil {
			t.Errorf("Check(%s) = %v", h, err)
		}
	}
	if got := len(hosts.Hosts()); got != 7 {
		t.Errorf("hosts %v, want exactly the 7 above", hosts.Hosts())
	}
	if err := hosts.Check("https://cas-bridge-direct.xethub.hf.co/file.bin"); err == nil {
		t.Error("cas-bridge-direct.xethub.hf.co is allowed")
	}
}

// Every call returns its own copy of the embedded bytes.
func TestEmbeddedCatalogIsACopy(t *testing.T) {
	data, err := (embeddedCatalog{}).Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'x'
	if again, err := (embeddedCatalog{}).Manifest(context.Background()); err != nil || again[0] != '{' {
		t.Fatalf("a changed copy leaked into the next call: %q, %v", again[:1], err)
	}
}
