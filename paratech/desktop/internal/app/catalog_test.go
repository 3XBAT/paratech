package app

import (
	"context"
	"slices"
	"strings"
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

// TASK-067, TASK-070: the embedded catalog lists every whisper size of the official repository in the full (f16) and
// the quantized (q5) variant, and the English-only ones where they exist, pinned to one commit with verified digests;
// small is the one preferred model.
func TestEmbeddedCatalog(t *testing.T) {
	cat := embeddedEntries(t)
	// size and sum: lfs.size and lfs.oid of the Hugging Face API at the pinned commit (2026-10-16, re-checked 2026-10-18).
	want := []struct {
		id     values.CatalogID
		file   string // the file name between "ggml-" and ".bin"
		family values.ModelFamily
		tier   values.ModelTier
		quant  values.Quantization
		load   values.ModelLoad
		size   int64
		sum    values.SHA256
	}{
		{"tiny-q5_1", "tiny-q5_1", values.FamilyTiny, values.TierFast, "q5_1", 1, 32152673,
			"818710568da3ca15689e31a743197b520007872ff9576237bda97bd1b469c3d7"},
		{"tiny-f16", "tiny", values.FamilyTiny, values.TierFast, "f16", 1, 77691713,
			"be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21"},
		{"base-q5_1", "base-q5_1", values.FamilyBase, values.TierFast, "q5_1", 1, 59707625,
			"422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898"},
		{"base-f16", "base", values.FamilyBase, values.TierFast, "f16", 2, 147951465,
			"60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe"},
		{"small-q5_1", "small-q5_1", values.FamilySmall, values.TierBalanced, "q5_1", 2, 190085487,
			"ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb"},
		{"small-f16", "small", values.FamilySmall, values.TierBalanced, "f16", 3, 487601967,
			"1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"},
		{"medium-q5_0", "medium-q5_0", values.FamilyMedium, values.TierAccurate, "q5_0", 3, 539212467,
			"19fea4b380c3a618ec4723c3eef2eb785ffba0d0538cf43f8f235e7b3b34220f"},
		{"medium-f16", "medium", values.FamilyMedium, values.TierAccurate, "f16", 4, 1533763059,
			"6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208"},
		{"large-v3-turbo-q5_0", "large-v3-turbo-q5_0", values.FamilyLargeV3Turbo, values.TierMax, "q5_0", 4, 574041195,
			"394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2"},
		{"large-v3-turbo-f16", "large-v3-turbo", values.FamilyLargeV3Turbo, values.TierMax, "f16", 5, 1624555275,
			"1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69"},
		{"large-v3-q5_0", "large-v3-q5_0", values.FamilyLargeV3, values.TierMax, "q5_0", 4, 1081140203,
			"d75795ecff3f83b5faa89d1900604ad8c780abd5739fae406de19f23ecd98ad1"},
		{"large-v3-f16", "large-v3", values.FamilyLargeV3, values.TierMax, "f16", 5, 3095033483,
			"64d182b440b98d5203c4f9bd541544d84c605196c4f7b845dfa11fb23594d1e2"},
		{"tiny.en-q5_1", "tiny.en-q5_1", values.FamilyTiny, values.TierFast, "q5_1", 1, 32166155,
			"c77c5766f1cef09b6b7d47f21b546cbddd4157886b3b5d6d4f709e91e66c7c2b"},
		{"tiny.en-f16", "tiny.en", values.FamilyTiny, values.TierFast, "f16", 1, 77704715,
			"921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f"},
		{"base.en-q5_1", "base.en-q5_1", values.FamilyBase, values.TierFast, "q5_1", 1, 59721011,
			"4baf70dd0d7c4247ba2b81fafd9c01005ac77c2f9ef064e00dcf195d0e2fdd2f"},
		{"base.en-f16", "base.en", values.FamilyBase, values.TierFast, "f16", 2, 147964211,
			"a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002"},
		{"small.en-q5_1", "small.en-q5_1", values.FamilySmall, values.TierBalanced, "q5_1", 2, 190098681,
			"bfdff4894dcb76bbf647d56263ea2a96645423f1669176f4844a1bf8e478ad30"},
		{"small.en-f16", "small.en", values.FamilySmall, values.TierBalanced, "f16", 3, 487614201,
			"c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d"},
		{"medium.en-q5_0", "medium.en-q5_0", values.FamilyMedium, values.TierAccurate, "q5_0", 3, 539225533,
			"76733e26ad8fe1c7a5bf7531a9d41917b2adc0f20f2e4f5531688a8c6cd88eb0"},
		{"medium.en-f16", "medium.en", values.FamilyMedium, values.TierAccurate, "f16", 4, 1533774781,
			"cc37e93478338ec7700281a7ac30a10128929eb8f427dda2e865faa8f6da4356"},
	}
	entries := cat.Entries()
	if len(entries) != len(want) {
		t.Fatalf("%d catalog entries, want %d", len(entries), len(want))
	}

	preferred := 0
	for i, w := range want {
		e := entries[i]
		english := strings.Contains(w.file, ".en")
		capability := values.CapabilityMultilingual
		if english {
			capability = values.CapabilityEnglish
		}
		src := e.SourceURL().String()
		switch {
		case e.ID() != w.id || string(e.Name()) != string(w.id) || e.Version() != "1":
			t.Errorf("entry %d = %s %s %s, want %s", i, e.ID(), e.Name(), e.Version(), w.id)
		case src != catalogSourcePrefix+w.file+".bin":
			t.Errorf("%s: source %s, want %s%s.bin", w.id, src, catalogSourcePrefix, w.file)
		case e.SourcePage().String() != catalogPagePrefix+w.file+".bin":
			t.Errorf("%s: source page %s", w.id, e.SourcePage())
		case e.SHA256() != w.sum || e.SizeBytes() != w.size:
			t.Errorf("%s: sha256 %s size %d, want %s %d", w.id, e.SHA256(), e.SizeBytes(), w.sum, w.size)
		case e.Tier() != w.tier || e.Quantization() != w.quant || e.Family() != w.family:
			t.Errorf("%s: tier %q quantization %q family %q, want %q %q %q", w.id, e.Tier(), e.Quantization(),
				e.Family(), w.tier, w.quant, w.family)
		case e.Load() != w.load || e.Load() < values.MinModelLoad || e.Load() > values.MaxModelLoad:
			t.Errorf("%s: load %d, want %d", w.id, e.Load(), w.load)
		case !slices.Equal(e.Capabilities(), []values.Capability{capability}):
			t.Errorf("%s: capabilities %v, want %v", w.id, e.Capabilities(), capability)
		case e.EnglishOnly() != english:
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

	// A heavier model never asks for less hardware, so the page can warn on a weaker machine; the heavy ones
	// (load 4 and 5) ask for 8 cores.
	for _, a := range entries {
		ra := a.Recommendation()
		if a.Load().Heavy() && ra.MinCPUCores < 8 {
			t.Errorf("%s: load %d with %d cores, want at least 8", a.ID(), a.Load(), ra.MinCPUCores)
		}
		for _, b := range entries {
			rb := b.Recommendation()
			if a.Load() < b.Load() && (ra.MinCPUCores > rb.MinCPUCores || ra.MinRAMMB > rb.MinRAMMB) {
				t.Errorf("%s (load %d) needs %+v, more than %s (load %d) %+v", a.ID(), a.Load(), ra, b.ID(), b.Load(), rb)
			}
		}
	}
}

// The six models of TASK-067 keep their identity and recommendation: an installed catalog model must stay recognized.
func TestEmbeddedCatalogKeepsTask067Entries(t *testing.T) {
	cat := embeddedEntries(t)
	for _, w := range []struct {
		id    values.CatalogID
		sum   values.SHA256
		cores int
		ramMB int64
	}{
		{"small-q5_1", "ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb", 4, 2048},
		{"base-q5_1", "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898", 2, 1024},
		{"medium-q5_0", "19fea4b380c3a618ec4723c3eef2eb785ffba0d0538cf43f8f235e7b3b34220f", 4, 3072},
		{"large-v3-turbo-q5_0", "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2", 8, 4096},
		{"base.en-q5_1", "4baf70dd0d7c4247ba2b81fafd9c01005ac77c2f9ef064e00dcf195d0e2fdd2f", 2, 1024},
		{"small.en-q5_1", "bfdff4894dcb76bbf647d56263ea2a96645423f1669176f4844a1bf8e478ad30", 4, 2048},
	} {
		e, ok := cat.Entry(w.id)
		if !ok {
			t.Errorf("%s is missing", w.id)
			continue
		}
		r := e.Recommendation()
		if string(e.Name()) != string(w.id) || e.Version() != "1" || e.SHA256() != w.sum || r.MinCPUCores != w.cores ||
			r.MinRAMMB != w.ramMB {
			t.Errorf("%s = %s %s %s %+v, want the TASK-067 entry", w.id, e.Name(), e.Version(), e.SHA256(), r)
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
