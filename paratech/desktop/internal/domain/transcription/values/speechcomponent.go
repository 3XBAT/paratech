package values

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Bounds of the speech component manifest (TASK-072). They only stop a broken manifest; the pinned sizes are what
// bounds a download and an unpack.
const (
	ComponentSchemaVersion           = 1
	MaxComponentManifestBytes        = 64 << 10
	MaxComponentFiles                = 64
	MaxComponentFileNameLen          = 64
	MaxComponentEntryPrefixLen       = 128
	MaxComponentArchiveBytes         = 512 << 20
	MaxComponentFileBytes      int64 = 256 << 20
	MaxComponentTotalBytes     int64 = 512 << 20
)

// ComponentFolder is a place where the speech component may be installed. The adapter maps it to a directory; the
// domain only knows the order in which the folders are tried.
type ComponentFolder int

// Folders of the speech component, in the order they are tried.
const (
	// FolderBundled is the folder of the executable: what the installer and the build scripts ship. Read only.
	FolderBundled ComponentFolder = iota + 1
	// FolderInstalled is the component folder below the data directory: what the in-app install writes.
	FolderInstalled
)

// ComponentFolders returns the folders in the order they are tried.
func ComponentFolders() []ComponentFolder { return []ComponentFolder{FolderBundled, FolderInstalled} }

// String names the folder for the log.
func (f ComponentFolder) String() string {
	switch f {
	case FolderBundled:
		return "bundled"
	case FolderInstalled:
		return "installed"
	}
	return fmt.Sprintf("folder(%d)", int(f))
}

// ComponentState is what a folder holds.
type ComponentState int

// States of the component in a folder.
const (
	// ComponentMissing: none of the files is there.
	ComponentMissing ComponentState = iota + 1
	// ComponentDamaged: some files are there, but the set does not verify.
	ComponentDamaged
	// ComponentReady: every file is a regular file with the pinned size and SHA-256.
	ComponentReady
)

// String names the state for the log.
func (s ComponentState) String() string {
	switch s {
	case ComponentMissing:
		return "missing"
	case ComponentDamaged:
		return "damaged"
	case ComponentReady:
		return "ready"
	}
	return fmt.Sprintf("state(%d)", int(s))
}

// ComponentFile is one file of the component with its pinned size and SHA-256. It comes from the archive, or, when it
// has a source of its own (Separate), is downloaded on its own from there (the VAD model, TASK-222).
type ComponentFile struct {
	name   string
	size   int64
	sum    SHA256
	source SourceURL
}

// Name returns the file name: lowercase, no path.
func (f ComponentFile) Name() string { return f.name }

// Size returns the pinned size in bytes.
func (f ComponentFile) Size() int64 { return f.size }

// SHA256 returns the pinned digest.
func (f ComponentFile) SHA256() SHA256 { return f.sum }

// Separate reports whether the file is downloaded on its own from Source instead of taken from the archive.
func (f ComponentFile) Separate() bool { return !f.source.IsZero() }

// Source returns the URL of a separate file; zero for a file of the archive.
func (f ComponentFile) Source() SourceURL { return f.source }

// CheckReply accepts only the whole file (a request from byte 0) of the pinned length, when the server names one.
func (f ComponentFile) CheckReply(r RangeReply) error {
	if r.Status != RangeFull {
		return fmt.Errorf("%w: %s: reply status %d, want the whole file", ErrUnexpectedRange, f.name, r.Status)
	}
	if r.Total != UnknownTotal && r.Total != f.size {
		return fmt.Errorf("%w: %s: source has %d bytes, want %d", ErrSizeMismatch, f.name, r.Total, f.size)
	}
	return nil
}

// Verify reports whether bytes with the given digest and size are this file.
func (f ComponentFile) Verify(sum SHA256, size int64) error {
	if size != f.size {
		return fmt.Errorf("%w: %s: %d bytes, want %d", ErrSizeMismatch, f.name, size, f.size)
	}
	if sum != f.sum {
		return fmt.Errorf("%w: %s", ErrChecksumMismatch, f.name)
	}
	return nil
}

// FoundFile is what an adapter found in a folder under the name of a component file. Regular is false for a link,
// a directory or anything else that is not a plain file; Size and SHA256 are set only for a regular file.
type FoundFile struct {
	Name    string
	Regular bool
	Size    int64
	SHA256  SHA256
}

// SpeechComponent is the pinned speech recognition component (whisper.cpp CLI and its libraries): the official release
// archive with its size and SHA-256, the hosts a download may reach, and the files with their sizes and SHA-256,
// taken from the archive or downloaded on their own (the VAD model). It is immutable.
type SpeechComponent struct {
	version    string
	source     SourceURL
	size       int64
	sum        SHA256
	hosts      HostAllowlist
	prefix     string
	executable string
	vadModel   string
	files      []ComponentFile
	total      int64
	separate   int64 // bytes of the separate files
}

// Version returns the release tag.
func (c SpeechComponent) Version() string { return c.version }

// SourceURL returns the archive URL.
func (c SpeechComponent) SourceURL() SourceURL { return c.source }

// ArchiveSize returns the pinned archive size.
func (c SpeechComponent) ArchiveSize() int64 { return c.size }

// ArchiveSHA256 returns the pinned archive digest.
func (c SpeechComponent) ArchiveSHA256() SHA256 { return c.sum }

// DownloadHosts returns the hosts a download and its redirects may reach: the manifest's list and the source host.
func (c SpeechComponent) DownloadHosts() HostAllowlist { return c.hosts }

// EntryPrefix returns the folder of the files inside the archive, for example "Release/".
func (c SpeechComponent) EntryPrefix() string { return c.prefix }

// Executable returns the name of the program file among Files.
func (c SpeechComponent) Executable() string { return c.executable }

// VADModel returns the name of the voice activity detection model among Files; "" when the component has none.
func (c SpeechComponent) VADModel() string { return c.vadModel }

// Files returns a copy of the files in manifest order.
func (c SpeechComponent) Files() []ComponentFile { return append([]ComponentFile(nil), c.files...) }

// ArchiveFiles returns the files taken from the archive, in manifest order.
func (c SpeechComponent) ArchiveFiles() []ComponentFile { return c.filesWhere(false) }

// SeparateFiles returns the files downloaded on their own, in manifest order.
func (c SpeechComponent) SeparateFiles() []ComponentFile { return c.filesWhere(true) }

func (c SpeechComponent) filesWhere(separate bool) []ComponentFile {
	var out []ComponentFile
	for _, f := range c.files {
		if f.Separate() == separate {
			out = append(out, f)
		}
	}
	return out
}

// TotalBytes returns the sum of the file sizes.
func (c SpeechComponent) TotalBytes() int64 { return c.total }

// DownloadBytes returns what an install downloads: the archive and the separate files.
func (c SpeechComponent) DownloadBytes() int64 { return c.size + c.separate }

// File returns the component file with exactly this name.
func (c SpeechComponent) File(name string) (ComponentFile, bool) {
	for _, f := range c.files {
		if f.name == name {
			return f, true
		}
	}
	return ComponentFile{}, false
}

// EntryFile returns the component file an archive entry holds: the entry name must be exactly the prefix followed by
// the name of a file of the archive. Every other entry is not part of the component.
func (c SpeechComponent) EntryFile(entry string) (ComponentFile, bool) {
	rest, ok := strings.CutPrefix(entry, c.prefix)
	if !ok {
		return ComponentFile{}, false
	}
	f, ok := c.File(rest)
	if !ok || f.Separate() {
		return ComponentFile{}, false
	}
	return f, true
}

// VerifyArchive reports whether a downloaded archive with the given digest and size is the pinned one.
func (c SpeechComponent) VerifyArchive(sum SHA256, size int64) error {
	if size != c.size {
		return fmt.Errorf("%w: component archive: %d bytes, want %d", ErrSizeMismatch, size, c.size)
	}
	if sum != c.sum {
		return fmt.Errorf("%w: component archive", ErrChecksumMismatch)
	}
	return nil
}

// CheckReply accepts only the whole archive (a request from byte 0) of the pinned length, when the server names one.
func (c SpeechComponent) CheckReply(r RangeReply) error {
	if r.Status != RangeFull {
		return fmt.Errorf("%w: component archive: reply status %d, want the whole file", ErrUnexpectedRange, r.Status)
	}
	if r.Total != UnknownTotal && r.Total != c.size {
		return fmt.Errorf("%w: component archive: source has %d bytes, want %d", ErrSizeMismatch, r.Total, c.size)
	}
	return nil
}

// RequiredSpace returns the free disk space an install needs: the downloads, the installed files and DiskSpaceMargin.
func (c SpeechComponent) RequiredSpace() int64 { return c.DownloadBytes() + c.total + DiskSpaceMargin }

// CheckDiskSpace reports whether free bytes are enough for an install; too little is an *InsufficientSpaceError.
func (c SpeechComponent) CheckDiskSpace(free int64) error {
	if need := c.RequiredSpace(); free < need {
		return &InsufficientSpaceError{Required: need, Available: free}
	}
	return nil
}

// Assess tells what a folder holds from the files found there under the component's names: nothing is
// ComponentMissing; every file regular with the pinned size and SHA-256 is ComponentReady; anything else is
// ComponentDamaged. Names that are not component files are ignored.
func (c SpeechComponent) Assess(found []FoundFile) ComponentState {
	seen := map[string]bool{}
	ok := true
	for _, f := range found {
		cf, known := c.File(f.Name)
		if !known {
			continue
		}
		if seen[f.Name] {
			ok = false
			continue
		}
		seen[f.Name] = true
		if !f.Regular || cf.Verify(f.SHA256, f.Size) != nil {
			ok = false
		}
	}
	switch {
	case len(seen) == 0:
		return ComponentMissing
	case ok && len(seen) == len(c.files):
		return ComponentReady
	}
	return ComponentDamaged
}

// CombineFolders picks the state of the component from the states of the folders in ComponentFolders order: the
// first ready folder wins; otherwise the component is damaged when any folder is, and missing when none has a file.
// index is the position of the ready folder, -1 when there is none.
func CombineFolders(states []ComponentState) (state ComponentState, index int) {
	state = ComponentMissing
	for i, s := range states {
		switch s {
		case ComponentReady:
			return ComponentReady, i
		case ComponentDamaged:
			state = ComponentDamaged
		}
	}
	return state, -1
}

// componentDoc is the JSON form of the manifest, kept flat so that the build scripts read the same file:
//
//	{"schema_version": 1, "version": "b5454", "source_url": "https://host/path/archive.zip", "size_bytes": 8928640,
//	 "sha256": "<64 hex>", "download_hosts": ["host"], "entry_prefix": "Release/", "executable": "whisper-cli.exe",
//	 "vad_model": "ggml-silero-v5.1.2.bin",
//	 "files": [{"name": "whisper-cli.exe", "size_bytes": 479744, "sha256": "<64 hex>"},
//	           {"name": "ggml-silero-v5.1.2.bin", "size_bytes": 885098, "sha256": "<64 hex>", "source_url": "https://…"}]}
type componentDoc struct {
	SchemaVersion int                `json:"schema_version"`
	Version       string             `json:"version"`
	SourceURL     string             `json:"source_url"`
	SizeBytes     int64              `json:"size_bytes"`
	SHA256        string             `json:"sha256"`
	DownloadHosts []string           `json:"download_hosts"`
	EntryPrefix   *string            `json:"entry_prefix"`
	Executable    string             `json:"executable"`
	VADModel      string             `json:"vad_model"`
	Files         []componentFileDoc `json:"files"`
}

type componentFileDoc struct {
	Name      string  `json:"name"`
	SizeBytes int64   `json:"size_bytes"`
	SHA256    string  `json:"sha256"`
	SourceURL *string `json:"source_url"`
}

// ParseSpeechComponent parses and validates the manifest. It is as strict as ParseCatalog: valid UTF-8 of at most
// MaxComponentManifestBytes, one JSON object, only known lowercase keys, none repeated. Then: the schema version; a
// version label; an https source URL; an archive of 1..MaxComponentArchiveBytes with a SHA-256; valid download hosts;
// an entry prefix (required, may be "") of path segments each followed by "/"; 1..MaxComponentFiles files with unique
// lowercase names that are plain file names (no path, no Windows device name), sizes of 1..MaxComponentFileBytes,
// MaxComponentTotalBytes in all, a SHA-256 each and, for a file downloaded on its own, an https source URL whose host
// joins the allowed hosts; the executable is one of the files of the archive; the VAD model, when named, is one of
// the files.
func ParseSpeechComponent(data []byte) (SpeechComponent, error) {
	if len(data) > MaxComponentManifestBytes {
		return SpeechComponent{}, fmt.Errorf("%w: larger than %d bytes", ErrInvalidComponent, MaxComponentManifestBytes)
	}
	if !utf8.Valid(data) {
		return SpeechComponent{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidComponent)
	}
	if err := checkJSONShape(data); err != nil {
		return SpeechComponent{}, fmt.Errorf("%w: %w", ErrInvalidComponent, err)
	}

	var doc componentDoc
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return SpeechComponent{}, fmt.Errorf("%w: %w", ErrInvalidComponent, err)
	}
	c, err := doc.component()
	if err != nil {
		return SpeechComponent{}, fmt.Errorf("%w: %w", ErrInvalidComponent, err)
	}
	return c, nil
}

func (d componentDoc) component() (SpeechComponent, error) {
	if d.SchemaVersion != ComponentSchemaVersion {
		return SpeechComponent{}, fmt.Errorf("schema version %d, want %d", d.SchemaVersion, ComponentSchemaVersion)
	}
	if err := validateLabel(ErrInvalidModelVersion, d.Version, MaxModelVersionLen); err != nil {
		return SpeechComponent{}, fmt.Errorf("version: %w", err)
	}
	src, err := ParseSourceURL(d.SourceURL)
	if err != nil {
		return SpeechComponent{}, err
	}
	if d.SizeBytes <= 0 || d.SizeBytes > MaxComponentArchiveBytes {
		return SpeechComponent{}, fmt.Errorf("archive size %d, want 1..%d", d.SizeBytes, MaxComponentArchiveBytes)
	}
	sum, err := ParseSHA256(d.SHA256)
	if err != nil {
		return SpeechComponent{}, err
	}
	hosts, err := NewHostAllowlist(d.DownloadHosts)
	if err != nil {
		return SpeechComponent{}, fmt.Errorf("download_hosts: %w", err)
	}
	host, err := NormalizeHost(src.Host())
	if err != nil {
		return SpeechComponent{}, err
	}
	if d.EntryPrefix == nil {
		return SpeechComponent{}, fmt.Errorf("no entry_prefix")
	}
	if err := validateEntryPrefix(*d.EntryPrefix); err != nil {
		return SpeechComponent{}, err
	}

	files, total, err := componentFiles(d.Files)
	if err != nil {
		return SpeechComponent{}, err
	}
	extraHosts := []string{host}
	var separate int64
	for _, f := range files {
		if !f.Separate() {
			continue
		}
		h, err := NormalizeHost(f.source.Host())
		if err != nil {
			return SpeechComponent{}, fmt.Errorf("file %q: %w", f.name, err)
		}
		extraHosts = append(extraHosts, h)
		separate += f.size
	}
	c := SpeechComponent{
		version: d.Version, source: src, size: d.SizeBytes, sum: sum, hosts: hosts.with(extraHosts...),
		prefix: *d.EntryPrefix, executable: d.Executable, vadModel: d.VADModel, files: files, total: total,
		separate: separate,
	}
	if f, ok := c.File(d.Executable); !ok || f.Separate() {
		return SpeechComponent{}, fmt.Errorf("executable %q is not one of the files of the archive", d.Executable)
	}
	if _, ok := c.File(d.VADModel); d.VADModel != "" && !ok {
		return SpeechComponent{}, fmt.Errorf("vad_model %q is not one of the files", d.VADModel)
	}
	return c, nil
}

func componentFiles(docs []componentFileDoc) ([]ComponentFile, int64, error) {
	if len(docs) == 0 || len(docs) > MaxComponentFiles {
		return nil, 0, fmt.Errorf("%d files, want 1..%d", len(docs), MaxComponentFiles)
	}
	files := make([]ComponentFile, 0, len(docs))
	names := map[string]bool{}
	var total int64
	for i, f := range docs {
		if err := validateComponentFileName(f.Name); err != nil {
			return nil, 0, fmt.Errorf("file %d: %w", i, err)
		}
		if names[f.Name] {
			return nil, 0, fmt.Errorf("file %d: %q repeats", i, f.Name)
		}
		if f.SizeBytes <= 0 || f.SizeBytes > MaxComponentFileBytes {
			return nil, 0, fmt.Errorf("file %d: size %d, want 1..%d", i, f.SizeBytes, MaxComponentFileBytes)
		}
		sum, err := ParseSHA256(f.SHA256)
		if err != nil {
			return nil, 0, fmt.Errorf("file %d: %w", i, err)
		}
		var src SourceURL
		if f.SourceURL != nil {
			if src, err = ParseSourceURL(*f.SourceURL); err != nil {
				return nil, 0, fmt.Errorf("file %d: %w", i, err)
			}
		}
		total += f.SizeBytes
		if total > MaxComponentTotalBytes {
			return nil, 0, fmt.Errorf("files larger than %d bytes in all", MaxComponentTotalBytes)
		}
		names[f.Name] = true
		files = append(files, ComponentFile{name: f.Name, size: f.SizeBytes, sum: sum, source: src})
	}
	return files, total, nil
}

// validateComponentFileName accepts a plain lowercase file name that is the same on every file system: letters,
// digits and inner ".", "_", "-", starting and ending with a letter or digit, not a Windows device name.
func validateComponentFileName(name string) error {
	if name == "" || len(name) > MaxComponentFileNameLen {
		return fmt.Errorf("file name empty or longer than %d bytes", MaxComponentFileNameLen)
	}
	for i, r := range name {
		if isLowerAlnum(r) {
			continue
		}
		if i > 0 && i < len(name)-1 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return fmt.Errorf("file name %q: only lowercase letters, digits and inner '.', '_', '-'", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("file name %q: '..'", name)
	}
	base, _, _ := strings.Cut(name, ".")
	if isWindowsDeviceName(base) {
		return fmt.Errorf("file name %q is a Windows device name", name)
	}
	return nil
}

// isWindowsDeviceName reports names Windows maps to devices whatever the extension ("nul.dll" is NUL).
func isWindowsDeviceName(base string) bool {
	switch base {
	case "con", "prn", "aux", "nul":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) {
		return base[3] >= '0' && base[3] <= '9'
	}
	return false
}

// validateEntryPrefix accepts "" or segments of letters, digits and inner ".", "_", "-", each followed by "/".
func validateEntryPrefix(p string) error {
	if p == "" {
		return nil
	}
	if len(p) > MaxComponentEntryPrefixLen || !strings.HasSuffix(p, "/") {
		return fmt.Errorf("entry_prefix %q: segments ending with '/', at most %d bytes", p, MaxComponentEntryPrefixLen)
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		if err := validateLabel(ErrInvalidComponent, seg, MaxComponentEntryPrefixLen); err != nil || strings.Contains(seg, "..") {
			return fmt.Errorf("entry_prefix %q: bad segment %q", p, seg)
		}
	}
	return nil
}
