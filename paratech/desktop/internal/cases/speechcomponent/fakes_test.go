package speechcomponent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

// archiveBytes stands for the release archive; the fake Unpack does not parse it.
var archiveBytes = bytes.Repeat([]byte("zip!"), 256)

func digest(b []byte) values.SHA256 {
	s := sha256.Sum256(b)
	return values.SHA256(hex.EncodeToString(s[:]))
}

// testFiles are the component files of testSpec.
var testFiles = map[string][]byte{"whisper-cli.exe": []byte("program"), "whisper.dll": []byte("library")}

// testSpec is a component of two files whose archive is archiveBytes.
func testSpec(t *testing.T) values.SpeechComponent {
	t.Helper()
	doc := fmt.Sprintf(`{"schema_version": 1, "version": "b1", "source_url": "https://example.com/r/archive.zip",
 "size_bytes": %d, "sha256": %q, "download_hosts": ["cdn.example.com"], "entry_prefix": "Release/",
 "executable": "whisper-cli.exe", "files": [
 {"name": "whisper-cli.exe", "size_bytes": %d, "sha256": %q},
 {"name": "whisper.dll", "size_bytes": %d, "sha256": %q}]}`,
		len(archiveBytes), digest(archiveBytes),
		len(testFiles["whisper-cli.exe"]), digest(testFiles["whisper-cli.exe"]),
		len(testFiles["whisper.dll"]), digest(testFiles["whisper.dll"]))
	spec, err := values.ParseSpeechComponent([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// verifiedSet is what a folder with the verified files holds.
func verifiedSet() []values.FoundFile {
	var out []values.FoundFile
	for name, b := range testFiles {
		out = append(out, values.FoundFile{Name: name, Regular: true, Size: int64(len(b)), SHA256: digest(b)})
	}
	return out
}

// damagedSet has one file with other content.
func damagedSet() []values.FoundFile {
	set := verifiedSet()
	set[0].SHA256 = digest([]byte("other"))
	return set
}

// fakeFiles keeps the folders in memory.
type fakeFiles struct {
	mu         sync.Mutex
	folders    map[values.ComponentFolder][]values.FoundFile
	inspectErr map[values.ComponentFolder]error
	archive    []byte
	saved      bool
	unpacked   int
	discarded  int
	unpackErr  error
	unpackSet  []values.FoundFile // what Unpack puts into the installed folder; nil: the verified set
	saveErr    error
	separate   map[string][]byte // stored separate files by name
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{
		folders: map[values.ComponentFolder][]values.FoundFile{}, inspectErr: map[values.ComponentFolder]error{},
		separate: map[string][]byte{},
	}
}

func (f *fakeFiles) SaveFile(_ context.Context, cf values.ComponentFile, content io.Reader) (int64, values.SHA256, error) {
	b, err := io.ReadAll(content)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		return int64(len(b)), "", err
	}
	f.separate[cf.Name()] = b
	return int64(len(b)), digest(b), nil
}

func (f *fakeFiles) Inspect(_ context.Context, folder values.ComponentFolder, _ values.SpeechComponent) ([]values.FoundFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.inspectErr[folder]; err != nil {
		return nil, err
	}
	return append([]values.FoundFile(nil), f.folders[folder]...), nil
}

func (f *fakeFiles) SaveArchive(_ context.Context, content io.Reader) (int64, values.SHA256, error) {
	b, err := io.ReadAll(content)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		return int64(len(b)), "", err
	}
	if f.saveErr != nil {
		return 0, "", f.saveErr
	}
	f.archive, f.saved = b, true
	return int64(len(b)), digest(b), nil
}

func (f *fakeFiles) Unpack(_ context.Context, spec values.SpeechComponent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unpacked++
	if f.unpackErr != nil {
		return f.unpackErr
	}
	// Unpack is only ever called on a verified archive.
	if err := spec.VerifyArchive(digest(f.archive), int64(len(f.archive))); err != nil {
		return fmt.Errorf("unpack of an unverified archive: %w", err)
	}
	for _, sf := range spec.SeparateFiles() {
		b := f.separate[sf.Name()]
		if err := sf.Verify(digest(b), int64(len(b))); err != nil {
			return fmt.Errorf("unpack of an unverified separate file: %w", err)
		}
	}
	set := f.unpackSet
	if set == nil {
		set = verifiedSet()
	}
	f.folders[values.FolderInstalled] = set
	return nil
}

func (f *fakeFiles) DiscardArchive(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discarded++
	f.archive = nil
	clear(f.separate)
	return nil
}

func (f *fakeFiles) counts() (unpacked, discarded int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unpacked, f.discarded
}

// fakeSource serves one reply; body defaults to archiveBytes.
type fakeSource struct {
	mu      sync.Mutex
	reply   values.RangeReply
	body    func(ctx context.Context) io.ReadCloser
	err     error
	calls   int
	offsets []int64
	allow   values.HostAllowlist
	entered chan struct{}     // closed on the first call, if set
	others  map[string][]byte // whole files served for other URLs than the archive
	urls    []string
}

func newFakeSource() *fakeSource {
	return &fakeSource{reply: values.RangeReply{Status: values.RangeFull, Total: int64(len(archiveBytes))}}
}

func (s *fakeSource) Fetch(ctx context.Context, src values.SourceURL, offset int64, allow values.HostAllowlist) (values.RangeReply, io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.offsets = append(s.offsets, offset)
	s.urls = append(s.urls, src.String())
	s.allow = allow
	if b, ok := s.others[src.String()]; ok {
		return values.RangeReply{Status: values.RangeFull, Total: int64(len(b))}, io.NopCloser(bytes.NewReader(b)), nil
	}
	if s.entered != nil && s.calls == 1 {
		close(s.entered)
	}
	if s.err != nil {
		return values.RangeReply{}, nil, s.err
	}
	if s.body != nil {
		return s.reply, s.body(ctx), nil
	}
	return s.reply, io.NopCloser(bytes.NewReader(archiveBytes)), nil
}

func (s *fakeSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// blockingBody sends part, then blocks until ctx ends.
type blockingBody struct {
	ctx  context.Context
	part []byte
	sent bool
}

func (b *blockingBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, b.part), nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingBody) Close() error { return nil }

// failingBody sends part, then fails like a broken connection.
type failingBody struct {
	part []byte
	sent bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, b.part), nil
	}
	return 0, errors.New("connection reset")
}

func (b *failingBody) Close() error { return nil }

type fakeSpace struct {
	free int64
	err  error
}

func (s fakeSpace) FreeBytes(context.Context) (int64, error) { return s.free, s.err }

// fakeActivator records the activations.
type fakeActivator struct {
	mu      sync.Mutex
	folders []values.ComponentFolder
	err     error
}

func (a *fakeActivator) Activate(_ context.Context, folder values.ComponentFolder) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.folders = append(a.folders, folder)
	return nil
}

func (a *fakeActivator) activated() []values.ComponentFolder {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]values.ComponentFolder(nil), a.folders...)
}
