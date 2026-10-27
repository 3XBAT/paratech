package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	compSumA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	compSumB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	compSumC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// componentJSON is a valid manifest with the given replacements applied to its text.
func componentJSON(replace ...string) string {
	doc := `{"schema_version": 1, "version": "b5454",
 "source_url": "https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip",
 "size_bytes": 1000, "sha256": "` + compSumA + `",
 "download_hosts": ["release-assets.githubusercontent.com"],
 "entry_prefix": "Release/", "executable": "whisper-cli.exe",
 "files": [{"name": "whisper-cli.exe", "size_bytes": 10, "sha256": "` + compSumB + `"},
           {"name": "ggml.dll", "size_bytes": 20, "sha256": "` + compSumC + `"}]}`
	return strings.NewReplacer(replace...).Replace(doc)
}

func mustComponent(t *testing.T, doc string) SpeechComponent {
	t.Helper()
	c, err := ParseSpeechComponent([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseSpeechComponent(t *testing.T) {
	c := mustComponent(t, componentJSON())
	if c.Version() != "b5454" || c.ArchiveSize() != 1000 || c.ArchiveSHA256() != compSumA || c.EntryPrefix() != "Release/" ||
		c.Executable() != "whisper-cli.exe" || len(c.Files()) != 2 || c.TotalBytes() != 30 {
		t.Fatalf("parsed %+v", c)
	}
	if got := strings.Join(c.DownloadHosts().Hosts(), ","); got != "github.com,release-assets.githubusercontent.com" {
		t.Fatalf("hosts = %s, want the listed host and the source host", got)
	}

	tests := []struct {
		name string
		doc  string
	}{
		{"schema", componentJSON(`"schema_version": 1`, `"schema_version": 2`)},
		{"no version", componentJSON(`"version": "b5454"`, `"version": ""`)},
		{"http source", componentJSON("https://github.com", "http://github.com")},
		{"source with query", componentJSON("x64.zip", "x64.zip?token=1")},
		{"archive size zero", componentJSON(`"size_bytes": 1000`, `"size_bytes": 0`)},
		{"archive too large", componentJSON(`"size_bytes": 1000`, fmt.Sprintf(`"size_bytes": %d`, MaxComponentArchiveBytes+1))},
		{"bad sha", componentJSON(compSumA, "xyz")},
		{"bad host", componentJSON("release-assets.githubusercontent.com", "bad host")},
		{"no prefix", componentJSON(`"entry_prefix": "Release/", `, "")},
		{"prefix without slash", componentJSON(`"Release/"`, `"Release"`)},
		{"prefix dot dot", componentJSON(`"Release/"`, `"../"`)},
		{"prefix absolute", componentJSON(`"Release/"`, `"/Release/"`)},
		{"prefix backslash", componentJSON(`"Release/"`, `"Release\\"`)},
		{"executable not a file", componentJSON(`"executable": "whisper-cli.exe"`, `"executable": "main.exe"`)},
		{"no files", componentJSON(`{"name": "whisper-cli.exe", "size_bytes": 10, "sha256": "`+compSumB+`"},`, "", `{"name": "ggml.dll", "size_bytes": 20, "sha256": "`+compSumC+`"}`, "")},
		{"repeated file", componentJSON(`"ggml.dll"`, `"whisper-cli.exe"`)},
		{"path in name", componentJSON(`"ggml.dll"`, `"sub/ggml.dll"`)},
		{"backslash in name", componentJSON(`"ggml.dll"`, `"sub\\ggml.dll"`)},
		{"dot dot name", componentJSON(`"ggml.dll"`, `"a..dll"`)},
		{"upper case name", componentJSON(`"ggml.dll"`, `"GGML.dll"`)},
		{"leading dot", componentJSON(`"ggml.dll"`, `".ggml.dll"`)},
		{"device name", componentJSON(`"ggml.dll"`, `"nul.dll"`)},
		{"device name com", componentJSON(`"ggml.dll"`, `"com1.dll"`)},
		{"file size zero", componentJSON(`"size_bytes": 20`, `"size_bytes": 0`)},
		{"file too large", componentJSON(`"size_bytes": 20`, fmt.Sprintf(`"size_bytes": %d`, MaxComponentFileBytes+1))},
		{"unknown key", componentJSON(`"version"`, `"extra": 1, "version"`)},
		{"repeated key", componentJSON(`"version": "b5454"`, `"version": "b5454", "version": "b1"`)},
		{"case folded key", componentJSON(`"version"`, `"VERSION"`)},
		{"trailing data", componentJSON() + "{}"},
		{"not utf-8", componentJSON("b5454", "b5454\xff")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseSpeechComponent([]byte(tt.doc)); !errors.Is(err, ErrInvalidComponent) {
				t.Fatalf("ParseSpeechComponent = %v, want ErrInvalidComponent", err)
			}
		})
	}

	if _, err := ParseSpeechComponent([]byte(strings.Repeat(" ", MaxComponentManifestBytes+1))); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("oversized manifest: %v", err)
	}
}

// vadJSON is componentJSON with a VAD model downloaded on its own.
func vadJSON(replace ...string) string {
	doc := componentJSON(`"executable": "whisper-cli.exe",`, `"executable": "whisper-cli.exe", "vad_model": "vad.bin",`,
		`"sha256": "`+compSumC+`"}]`,
		`"sha256": "`+compSumC+`"}, {"name": "vad.bin", "size_bytes": 5, "sha256": "`+compSumA+
			`", "source_url": "https://huggingface.co/org/vad/resolve/main/vad.bin"}]`)
	return strings.NewReplacer(replace...).Replace(doc)
}

func TestSpeechComponentSeparateFiles(t *testing.T) {
	c := mustComponent(t, vadJSON())
	if c.VADModel() != "vad.bin" || len(c.Files()) != 3 || c.TotalBytes() != 35 || c.DownloadBytes() != 1005 {
		t.Fatalf("parsed %+v", c)
	}
	if got := len(c.ArchiveFiles()); got != 2 {
		t.Fatalf("ArchiveFiles = %d, want 2", got)
	}
	sep := c.SeparateFiles()
	if len(sep) != 1 || sep[0].Name() != "vad.bin" || !sep[0].Separate() ||
		sep[0].Source().String() != "https://huggingface.co/org/vad/resolve/main/vad.bin" {
		t.Fatalf("SeparateFiles = %+v", sep)
	}
	if f, _ := c.File("ggml.dll"); f.Separate() || !f.Source().IsZero() {
		t.Fatalf("a file of the archive has a source: %+v", f)
	}
	if got := strings.Join(c.DownloadHosts().Hosts(), ","); got != "github.com,huggingface.co,release-assets.githubusercontent.com" {
		t.Fatalf("hosts = %s, want the source hosts of the separate files too", got)
	}
	if _, ok := c.EntryFile("Release/vad.bin"); ok {
		t.Fatal("a separate file must never come from the archive")
	}
	if need := int64(1005+35) + DiskSpaceMargin; c.RequiredSpace() != need {
		t.Fatalf("RequiredSpace = %d, want %d", c.RequiredSpace(), need)
	}
	if mustComponent(t, componentJSON()).VADModel() != "" {
		t.Fatal("a manifest without vad_model must have no VAD model")
	}

	bad := []struct {
		name string
		doc  string
	}{
		{"vad model not a file", vadJSON(`"vad_model": "vad.bin"`, `"vad_model": "other.bin"`)},
		{"http source", vadJSON("https://huggingface.co", "http://huggingface.co")},
		{"empty source", vadJSON("https://huggingface.co/org/vad/resolve/main/vad.bin", "")},
		{"executable downloaded on its own", vadJSON(`"executable": "whisper-cli.exe"`, `"executable": "vad.bin"`)},
		{"unknown file key", vadJSON(`"source_url": "https://huggingface.co`, `"url": "https://huggingface.co`)},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseSpeechComponent([]byte(tt.doc)); !errors.Is(err, ErrInvalidComponent) {
				t.Fatalf("ParseSpeechComponent = %v, want ErrInvalidComponent", err)
			}
		})
	}

	replies := []struct {
		reply RangeReply
		want  error
	}{
		{RangeReply{Status: RangeFull, Total: 5}, nil},
		{RangeReply{Status: RangeFull, Total: UnknownTotal}, nil},
		{RangeReply{Status: RangeFull, Total: 6}, ErrSizeMismatch},
		{RangeReply{Status: RangePartial, Start: 0, Total: 5}, ErrUnexpectedRange},
	}
	for _, tt := range replies {
		if err := sep[0].CheckReply(tt.reply); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
			t.Errorf("CheckReply(%+v) = %v, want %v", tt.reply, err, tt.want)
		}
	}
}

func TestComponentTotalLimit(t *testing.T) {
	var files []string
	for i := range 5 {
		files = append(files, fmt.Sprintf(`{"name": "f%d.dll", "size_bytes": %d, "sha256": "%s"}`, i, MaxComponentFileBytes, compSumB))
	}
	doc := componentJSON(`"executable": "whisper-cli.exe"`, `"executable": "f0.dll"`)
	doc = doc[:strings.Index(doc, `"files": [`)] + `"files": [` + strings.Join(files, ",") + `]}`
	if _, err := ParseSpeechComponent([]byte(doc)); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("files above the total = %v, want ErrInvalidComponent", err)
	}
}

func TestEntryFileMatchesExactNamesOnly(t *testing.T) {
	c := mustComponent(t, componentJSON())
	tests := []struct {
		entry string
		want  string
	}{
		{"Release/whisper-cli.exe", "whisper-cli.exe"},
		{"Release/ggml.dll", "ggml.dll"},
		{"whisper-cli.exe", ""},
		{"Release/WHISPER-CLI.EXE", ""},
		{"release/whisper-cli.exe", ""},
		{"Release/../whisper-cli.exe", ""},
		{"Release\\whisper-cli.exe", ""},
		{"/Release/whisper-cli.exe", ""},
		{"Release/sub/whisper-cli.exe", ""},
		{"Release/main.exe", ""},
		{"Release/", ""},
	}
	for _, tt := range tests {
		f, ok := c.EntryFile(tt.entry)
		if got := f.Name(); ok != (tt.want != "") || got != tt.want {
			t.Errorf("EntryFile(%q) = %q, %v; want %q", tt.entry, got, ok, tt.want)
		}
	}
}

func TestAssessComponentFolder(t *testing.T) {
	c := mustComponent(t, componentJSON())
	exe := FoundFile{Name: "whisper-cli.exe", Regular: true, Size: 10, SHA256: compSumB}
	dll := FoundFile{Name: "ggml.dll", Regular: true, Size: 20, SHA256: compSumC}
	tests := []struct {
		name  string
		found []FoundFile
		want  ComponentState
	}{
		{"nothing", nil, ComponentMissing},
		{"only other names", []FoundFile{{Name: "readme.txt", Regular: true}}, ComponentMissing},
		{"complete", []FoundFile{exe, dll}, ComponentReady},
		{"complete with extras", []FoundFile{exe, dll, {Name: "main.exe", Regular: true}}, ComponentReady},
		{"one missing", []FoundFile{exe}, ComponentDamaged},
		{"wrong size", []FoundFile{exe, {Name: "ggml.dll", Regular: true, Size: 21, SHA256: compSumC}}, ComponentDamaged},
		{"wrong digest", []FoundFile{exe, {Name: "ggml.dll", Regular: true, Size: 20, SHA256: compSumA}}, ComponentDamaged},
		{"a link", []FoundFile{exe, {Name: "ggml.dll", Regular: false}}, ComponentDamaged},
		{"reported twice", []FoundFile{exe, dll, dll}, ComponentDamaged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Assess(tt.found); got != tt.want {
				t.Fatalf("Assess = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCombineFolders(t *testing.T) {
	tests := []struct {
		states []ComponentState
		want   ComponentState
		index  int
	}{
		{nil, ComponentMissing, -1},
		{[]ComponentState{ComponentMissing, ComponentMissing}, ComponentMissing, -1},
		{[]ComponentState{ComponentReady, ComponentDamaged}, ComponentReady, 0},
		{[]ComponentState{ComponentDamaged, ComponentReady}, ComponentReady, 1},
		{[]ComponentState{ComponentDamaged, ComponentMissing}, ComponentDamaged, -1},
		{[]ComponentState{ComponentMissing, ComponentDamaged}, ComponentDamaged, -1},
	}
	for _, tt := range tests {
		if got, i := CombineFolders(tt.states); got != tt.want || i != tt.index {
			t.Errorf("CombineFolders(%v) = %v, %d; want %v, %d", tt.states, got, i, tt.want, tt.index)
		}
	}
	if f := ComponentFolders(); len(f) != 2 || f[0] != FolderBundled || f[1] != FolderInstalled {
		t.Fatalf("ComponentFolders = %v, want the bundled folder first", f)
	}
}

func TestComponentArchiveChecks(t *testing.T) {
	c := mustComponent(t, componentJSON())
	if err := c.VerifyArchive(compSumA, 1000); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyArchive(compSumA, 999); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("short archive: %v", err)
	}
	if err := c.VerifyArchive(compSumB, 1000); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("other archive: %v", err)
	}

	replies := []struct {
		reply RangeReply
		want  error
	}{
		{RangeReply{Status: RangeFull, Total: 1000}, nil},
		{RangeReply{Status: RangeFull, Total: UnknownTotal}, nil},
		{RangeReply{Status: RangeFull, Total: 1001}, ErrSizeMismatch},
		{RangeReply{Status: RangePartial, Start: 0, Total: 1000}, ErrUnexpectedRange},
		{RangeReply{Status: RangeNotSatisfiable, Total: UnknownTotal}, ErrUnexpectedRange},
	}
	for _, tt := range replies {
		if err := c.CheckReply(tt.reply); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
			t.Errorf("CheckReply(%+v) = %v, want %v", tt.reply, err, tt.want)
		}
	}

	need := int64(1000+30) + DiskSpaceMargin
	if c.RequiredSpace() != need || c.CheckDiskSpace(need) != nil {
		t.Fatalf("RequiredSpace = %d, want %d", c.RequiredSpace(), need)
	}
	var space *InsufficientSpaceError
	if err := c.CheckDiskSpace(need - 1); !errors.As(err, &space) || space.Required != need {
		t.Fatalf("CheckDiskSpace(need-1) = %v", err)
	}
}

func TestComponentFileVerify(t *testing.T) {
	c := mustComponent(t, componentJSON())
	f, ok := c.File("ggml.dll")
	if !ok || f.Size() != 20 || f.SHA256() != compSumC {
		t.Fatalf("File = %+v, %v", f, ok)
	}
	if err := f.Verify(compSumC, 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Verify(compSumC, 19); !errors.Is(err, ErrSizeMismatch) {
		t.Fatal(err)
	}
	if err := f.Verify(compSumA, 20); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatal(err)
	}
}
