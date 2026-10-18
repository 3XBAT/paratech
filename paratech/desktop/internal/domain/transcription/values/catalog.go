package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// Bounds of the catalog manifest.
const (
	CatalogSchemaVersion = 1
	MaxCatalogBytes      = 1 << 20
	MaxCatalogEntries    = 256
	MaxRecommendedCores  = 1024
	MaxRecommendedRAMMB  = 1 << 22 // 4 TiB
	maxCatalogDepth      = 8       // the schema nests 4 levels deep
)

// Recommendation is the hardware a catalog model is meant for (PRD section 8.1). Preferred marks the default
// recommendations for CPU-only machines (small quantized models); the rest are an opt-in choice.
type Recommendation struct {
	MinCPUCores int
	MinRAMMB    int64
	Preferred   bool
}

func (r Recommendation) validate() error {
	if r.MinCPUCores < 0 || r.MinCPUCores > MaxRecommendedCores || r.MinRAMMB < 0 || r.MinRAMMB > MaxRecommendedRAMMB {
		return fmt.Errorf("recommendation out of range: %d cores, %d MB", r.MinCPUCores, r.MinRAMMB)
	}
	return nil
}

// CatalogEntrySpec is the input of NewCatalogEntry.
type CatalogEntrySpec struct {
	ID             CatalogID
	Name           ModelName
	Version        ModelVersion
	SizeBytes      int64
	SHA256         SHA256
	SourceURL      SourceURL
	SourcePage     SourceURL // optional: the page to open in the browser for a manual download
	Quantization   Quantization
	Capabilities   []Capability
	Recommendation Recommendation
	Tier           ModelTier   // optional
	Family         ModelFamily // optional
}

// CatalogEntry is one downloadable model of the catalog manifest (PRD section 8.1). It is immutable.
type CatalogEntry struct {
	id             CatalogID
	name           ModelName
	version        ModelVersion
	size           int64
	sha256         SHA256
	source         SourceURL
	page           SourceURL
	quantization   Quantization
	capabilities   []Capability
	recommendation Recommendation
	tier           ModelTier
	family         ModelFamily
}

// NewCatalogEntry validates every field. ID, name, version, size, SHA-256 and the source URL are required; the source
// page, the tier and the family are optional.
func NewCatalogEntry(s CatalogEntrySpec) (CatalogEntry, error) {
	if s.SourceURL.IsZero() {
		return CatalogEntry{}, fmt.Errorf("%w: no source url", ErrInvalidSourceURL)
	}
	for _, err := range []error{
		s.ID.Validate(), s.Name.Validate(), s.Version.Validate(), ValidateSize(s.SizeBytes), s.SHA256.Validate(),
		s.Quantization.Validate(), ValidateCapabilities(s.Capabilities), s.Recommendation.validate(),
		s.Tier.Validate(), s.Family.Validate(),
	} {
		if err != nil {
			return CatalogEntry{}, err
		}
	}

	return CatalogEntry{
		id: s.ID, name: s.Name, version: s.Version, size: s.SizeBytes, sha256: s.SHA256, source: s.SourceURL,
		page: s.SourcePage, quantization: s.Quantization, capabilities: append([]Capability(nil), s.Capabilities...),
		recommendation: s.Recommendation, tier: s.Tier, family: s.Family,
	}, nil
}

// ID returns the catalog id of the entry.
func (e CatalogEntry) ID() CatalogID { return e.id }

// Name returns the model name.
func (e CatalogEntry) Name() ModelName { return e.name }

// Version returns the model version.
func (e CatalogEntry) Version() ModelVersion { return e.version }

// SizeBytes returns the size of the model file.
func (e CatalogEntry) SizeBytes() int64 { return e.size }

// SHA256 returns the expected digest of the model file.
func (e CatalogEntry) SHA256() SHA256 { return e.sha256 }

// SourceURL returns where the file can be downloaded.
func (e CatalogEntry) SourceURL() SourceURL { return e.source }

// SourcePage returns the page the user opens in the browser to download the file by hand (PRD section 8.1: open
// source page); the source URL when the manifest names no page. It is data for the browser only: its host is not
// added to the download allowlist.
func (e CatalogEntry) SourcePage() SourceURL {
	if e.page.IsZero() {
		return e.source
	}
	return e.page
}

// Quantization returns the weight format, empty if unknown.
func (e CatalogEntry) Quantization() Quantization { return e.quantization }

// Capabilities returns a copy of the capability labels.
func (e CatalogEntry) Capabilities() []Capability {
	return append([]Capability(nil), e.capabilities...)
}

// Recommendation returns the hardware the model is meant for.
func (e CatalogEntry) Recommendation() Recommendation { return e.recommendation }

// Tier returns the speed and accuracy class, TierNone if the manifest names none.
func (e CatalogEntry) Tier() ModelTier { return e.tier }

// Family returns the model family, FamilyNone if the manifest names none.
func (e CatalogEntry) Family() ModelFamily { return e.family }

// Load returns how hard the model works the computer, by LoadOf from the family and the quantization, so it cannot
// contradict the rule; LoadUnknown without a family.
func (e CatalogEntry) Load() ModelLoad { return LoadOf(e.family, e.quantization) }

// Heavy reports whether the load gets the "demanding model" warning (ModelLoad.Heavy).
func (e CatalogEntry) Heavy() bool { return e.Load().Heavy() }

// Reduced reports whether the entry is a quantized variant (Quantization.Reduced).
func (e CatalogEntry) Reduced() bool { return e.quantization.Reduced() }

// EnglishOnly reports whether the model recognizes English only: it has the English capability and not the
// multilingual one.
func (e CatalogEntry) EnglishOnly() bool {
	var en, multi bool
	for _, c := range e.capabilities {
		en = en || c == CapabilityEnglish
		multi = multi || c == CapabilityMultilingual
	}
	return en && !multi
}

// Verify reports whether a file with the given digest and size is this model (PRD section 8.1: a file that does not
// match is rejected and not installed).
func (e CatalogEntry) Verify(sum SHA256, size int64) error {
	if err := e.VerifySize(size); err != nil {
		return err
	}
	if sum != e.sha256 {
		return fmt.Errorf("%w: %s %s", ErrChecksumMismatch, e.name, e.version)
	}
	return nil
}

// VerifySize is the size part of Verify: a cheap check before a file is hashed.
func (e CatalogEntry) VerifySize(size int64) error {
	if size != e.size {
		return fmt.Errorf("%w: %s %s: %d bytes, want %d", ErrSizeMismatch, e.name, e.version, size, e.size)
	}
	return nil
}

// Catalog is the validated manifest of downloadable models, in manifest order (PRD section 8.1). Ids, name and
// version pairs and digests are unique.
type Catalog struct {
	entries []CatalogEntry
	hosts   HostAllowlist
}

// DownloadHosts returns the hosts a download may reach: the host of every entry's source URL and the extra hosts of
// the manifest's download_hosts (for example the CDN a source redirects to).
func (c Catalog) DownloadHosts() HostAllowlist { return c.hosts }

// Entries returns a copy of the entries.
func (c Catalog) Entries() []CatalogEntry { return append([]CatalogEntry(nil), c.entries...) }

// Entry returns the entry with the id.
func (c Catalog) Entry(id CatalogID) (CatalogEntry, bool) {
	for _, e := range c.entries {
		if e.id == id {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// CheckImportIdentity refuses a name and version of a catalog entry for a model imported outside the catalog
// (PRD section 8.1, AC-23). An unverified file must neither pass for the catalog model nor take the identity that the
// real catalog install needs (installed name and version pairs are unique).
func (c Catalog) CheckImportIdentity(name ModelName, version ModelVersion) error {
	for _, e := range c.entries {
		if e.name == name && e.version == version {
			return fmt.Errorf("%w: %s %s is catalog entry %s", ErrReservedIdentity, name, version, e.id)
		}
	}
	return nil
}

// manifestDoc is the JSON form of the manifest:
//
//	{"schema_version": 1, "models": [{"id": "base-q5_1", "name": "base", "version": "1",
//	  "size_bytes": 59707625, "sha256": "<64 hex>", "source_url": "https://host/path/file.bin",
//	  "source_page": "https://host/path", "quantization": "q5_1", "capabilities": ["multilingual"],
//	  "recommendation": {"min_cpu_cores": 2, "min_ram_mb": 1024, "preferred": true}, "tier": "fast",
//	  "family": "base"}],
//	 "download_hosts": ["cdn.example.com"]}
//
// download_hosts, source_page, tier and family are optional; the language is in capabilities ("en" = English only). models is required; "models": [] is a catalog without downloadable
// models. Models is a pointer so that a missing or null list is told apart from an empty one.
type manifestDoc struct {
	SchemaVersion int                 `json:"schema_version"`
	Models        *[]manifestEntryDoc `json:"models"`
	DownloadHosts []string            `json:"download_hosts"`
}

type manifestEntryDoc struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Version        string             `json:"version"`
	SizeBytes      int64              `json:"size_bytes"`
	SHA256         string             `json:"sha256"`
	SourceURL      string             `json:"source_url"`
	SourcePage     *string            `json:"source_page"`
	Quantization   string             `json:"quantization"`
	Capabilities   []string           `json:"capabilities"`
	Recommendation *recommendationDoc `json:"recommendation"`
	Tier           string             `json:"tier"`
	Family         string             `json:"family"`
}

type recommendationDoc struct {
	MinCPUCores int   `json:"min_cpu_cores"`
	MinRAMMB    int64 `json:"min_ram_mb"`
	Preferred   bool  `json:"preferred"`
}

// ParseCatalog parses and validates a manifest (PRD section 8.1). It is strict: valid UTF-8 of at most
// MaxCatalogBytes, exactly one JSON object, only known lowercase keys, no repeated key in any object, the expected
// schema version, a models list of 0..MaxCatalogEntries entries that pass NewCatalogEntry, and no two entries with the
// same id, the same name and version, or the same SHA-256. An explicit empty list is a valid catalog without
// downloadable models; a missing or null list is refused. download_hosts and the hosts of the source URLs must pass
// NewHostAllowlist. A source_page, when present, must pass ParseSourceURL like source_url. A tier, when present, must
// be one of the ModelTier constants in lowercase, a family one of the ModelFamily constants in lowercase.
func ParseCatalog(data []byte) (Catalog, error) {
	if len(data) > MaxCatalogBytes {
		return Catalog{}, fmt.Errorf("%w: larger than %d bytes", ErrInvalidCatalog, MaxCatalogBytes)
	}
	if !utf8.Valid(data) {
		return Catalog{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidCatalog)
	}
	if err := checkJSONShape(data); err != nil {
		return Catalog{}, fmt.Errorf("%w: %w", ErrInvalidCatalog, err)
	}

	var doc manifestDoc
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return Catalog{}, fmt.Errorf("%w: %w", ErrInvalidCatalog, err)
	}
	if doc.SchemaVersion != CatalogSchemaVersion {
		return Catalog{}, fmt.Errorf("%w: schema version %d, want %d", ErrInvalidCatalog, doc.SchemaVersion, CatalogSchemaVersion)
	}
	if doc.Models == nil {
		return Catalog{}, fmt.Errorf("%w: no models list", ErrInvalidCatalog)
	}
	models := *doc.Models
	if len(models) > MaxCatalogEntries {
		return Catalog{}, fmt.Errorf("%w: %d models, want at most %d", ErrInvalidCatalog, len(models), MaxCatalogEntries)
	}

	hosts, err := NewHostAllowlist(doc.DownloadHosts)
	if err != nil {
		return Catalog{}, fmt.Errorf("%w: download_hosts: %w", ErrInvalidCatalog, err)
	}

	entries := make([]CatalogEntry, 0, len(models))
	sourceHosts := make([]string, 0, len(models))
	ids := map[CatalogID]bool{}
	identities := map[[2]string]bool{}
	sums := map[SHA256]bool{}
	for i, m := range models {
		e, err := m.entry()
		if err != nil {
			return Catalog{}, fmt.Errorf("%w: model %d: %w", ErrInvalidCatalog, i, err)
		}

		identity := [2]string{string(e.name), string(e.version)}
		switch {
		case ids[e.id]:
			return Catalog{}, fmt.Errorf("%w: model %d: id %q repeats", ErrInvalidCatalog, i, e.id)
		case identities[identity]:
			return Catalog{}, fmt.Errorf("%w: model %d: %s %s repeats", ErrInvalidCatalog, i, e.name, e.version)
		case sums[e.sha256]:
			return Catalog{}, fmt.Errorf("%w: model %d: sha256 repeats", ErrInvalidCatalog, i)
		}
		host, err := NormalizeHost(e.source.Host())
		if err != nil {
			return Catalog{}, fmt.Errorf("%w: model %d: %w", ErrInvalidCatalog, i, err)
		}

		ids[e.id], identities[identity], sums[e.sha256] = true, true, true
		entries = append(entries, e)
		sourceHosts = append(sourceHosts, host)
	}
	return Catalog{entries: entries, hosts: hosts.with(sourceHosts...)}, nil
}

func (m manifestEntryDoc) entry() (CatalogEntry, error) {
	sum, err := ParseSHA256(m.SHA256)
	if err != nil {
		return CatalogEntry{}, err
	}
	src, err := ParseSourceURL(m.SourceURL)
	if err != nil {
		return CatalogEntry{}, err
	}
	var page SourceURL
	if m.SourcePage != nil {
		if page, err = ParseSourceURL(*m.SourcePage); err != nil {
			return CatalogEntry{}, fmt.Errorf("source_page: %w", err)
		}
	}
	caps := make([]Capability, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = Capability(c)
	}
	var rec Recommendation
	if m.Recommendation != nil {
		rec = Recommendation(*m.Recommendation)
	}

	return NewCatalogEntry(CatalogEntrySpec{
		ID: CatalogID(m.ID), Name: ModelName(m.Name), Version: ModelVersion(m.Version), SizeBytes: m.SizeBytes,
		SHA256: sum, SourceURL: src, SourcePage: page, Quantization: Quantization(m.Quantization), Capabilities: caps,
		Recommendation: rec, Tier: ModelTier(m.Tier), Family: ModelFamily(m.Family),
	})
}

// shapeFrame is one open object or array of checkJSONShape.
type shapeFrame struct {
	object  bool
	wantKey bool
	keys    map[string]bool
}

// checkJSONShape walks the tokens before decoding: encoding/json silently keeps the last of repeated keys and matches
// keys case-insensitively, so it alone cannot reject {"sha256": "a", "SHA256": "b"}. It also bounds the nesting and
// refuses anything after the single top-level object.
func checkJSONShape(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var stack []*shapeFrame
	done := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if done {
			return errors.New("data after the top-level object")
		}

		var top *shapeFrame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top != nil && top.object && top.wantKey {
			if tok == json.Delim('}') {
				stack = stack[:len(stack)-1]
				done = valueDone(stack)
				continue
			}
			key, _ := tok.(string) // the decoder only yields strings in key position
			if !isManifestKey(key) {
				return fmt.Errorf("key %q is not a lowercase name", key)
			}
			if top.keys[key] {
				return fmt.Errorf("key %q repeats", key)
			}
			top.keys[key] = true
			top.wantKey = false
			continue
		}

		switch tok {
		case json.Delim('{'), json.Delim('['):
			if len(stack) == 0 && tok != json.Delim('{') {
				return errors.New("the manifest is not a JSON object")
			}
			if len(stack) >= maxCatalogDepth {
				return fmt.Errorf("nested deeper than %d levels", maxCatalogDepth)
			}
			object := tok == json.Delim('{')
			stack = append(stack, &shapeFrame{object: object, wantKey: object, keys: map[string]bool{}})
		case json.Delim(']'):
			stack = stack[:len(stack)-1]
			done = valueDone(stack)
		default:
			if len(stack) == 0 {
				return errors.New("the manifest is not a JSON object")
			}
			done = valueDone(stack)
		}
	}
	if !done {
		return errors.New("empty manifest")
	}
	return nil
}

// valueDone records that a value ended inside the innermost open frame and reports whether it was the top level.
func valueDone(stack []*shapeFrame) bool {
	if len(stack) == 0 {
		return true
	}
	if top := stack[len(stack)-1]; top.object {
		top.wantKey = true
	}
	return false
}

func isManifestKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for _, r := range k {
		if !isLowerAlnum(r) && r != '_' {
			return false
		}
	}
	return true
}
