package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/componentstore"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/speechcomponent"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// testBinary is the test binary as the speech component: it doubles as the fake whisper-cli (TestMain). Hashed once.
var testBinary struct {
	once sync.Once
	name string
	size int64
	sum  string
	err  error
}

func hashTestBinary(t *testing.T, bin string) (name string, size int64, sum string) {
	t.Helper()
	testBinary.once.Do(func() {
		f, err := os.Open(bin)
		if err != nil {
			testBinary.err = err
			return
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		n, err := io.Copy(h, f)
		testBinary.name, testBinary.size, testBinary.sum, testBinary.err = filepath.Base(bin), n, hex.EncodeToString(h.Sum(nil)), err
	})
	if testBinary.err != nil {
		t.Fatal(testBinary.err)
	}
	return testBinary.name, testBinary.size, testBinary.sum
}

// testComponentManifest pins the test binary as the only component file of an archive with the given URL, size and
// digest.
func testComponentManifest(t *testing.T, bin, source string, archiveSize int, archiveSum string) []byte {
	t.Helper()
	name, size, sum := hashTestBinary(t, bin)
	return fmt.Appendf(nil, `{"schema_version": 1, "version": "test", "source_url": %q, "size_bytes": %d, "sha256": %q,
 "download_hosts": [], "entry_prefix": "Release/", "executable": %q,
 "files": [{"name": %q, "size_bytes": %d, "sha256": %q}]}`, source, archiveSize, archiveSum, name, name, size, sum)
}

// testSpeechEnv finds the test binary, verified, next to itself; an install would fail, no test reaches the network.
func testSpeechEnv(t *testing.T, bin string) speechEnv {
	t.Helper()
	return speechEnv{
		manifest:   testComponentManifest(t, bin, "https://example.invalid/speech.zip", 1, strings.Repeat("0", 64)),
		bundledDir: filepath.Dir(bin),
		source:     offlineSource{},
	}
}

// offlineSource refuses every download.
type offlineSource struct{}

func (offlineSource) Fetch(context.Context, values.SourceURL, int64, values.HostAllowlist) (values.RangeReply, io.ReadCloser, error) {
	return values.RangeReply{}, nil, errors.New("tests do not download")
}

// The shipped manifest pins the official whisper.cpp release of docs/refs/whisper.md: the archive, its hosts and the
// thirteen files the CLI needs.
func TestEmbeddedSpeechManifest(t *testing.T) {
	spec, err := values.ParseSpeechComponent(embeddedSpeechManifest())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Version() != "b5454" ||
		spec.SourceURL().String() != "https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip" ||
		spec.ArchiveSize() != 8928640 || spec.ArchiveSHA256() != "6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb" ||
		spec.EntryPrefix() != "Release/" || spec.Executable() != "whisper-cli.exe" {
		t.Fatalf("manifest %+v", spec)
	}
	wantHosts := "cas-bridge.xethub.hf.co,cdn-lfs-eu-1.hf.co,cdn-lfs-us-1.hf.co,cdn-lfs.hf.co,github.com,huggingface.co," +
		"objects.githubusercontent.com,release-assets.githubusercontent.com,transfer.xethub.hf.co,us.aws.cdn.hf.co"
	if got := strings.Join(spec.DownloadHosts().Hosts(), ","); got != wantHosts {
		t.Fatalf("hosts %s", got)
	}
	var names []string
	for _, f := range spec.ArchiveFiles() {
		names = append(names, f.Name())
	}
	want := "whisper-cli.exe,whisper.dll,ggml.dll,ggml-base.dll,ggml-cpu-alderlake.dll,ggml-cpu-cannonlake.dll," +
		"ggml-cpu-cascadelake.dll,ggml-cpu-haswell.dll,ggml-cpu-icelake.dll,ggml-cpu-sandybridge.dll," +
		"ggml-cpu-skylakex.dll,ggml-cpu-sse42.dll,ggml-cpu-x64.dll"
	if strings.Join(names, ",") != want || spec.TotalBytes() != 11603968+885098 {
		t.Fatalf("files %v (%d bytes)", names, spec.TotalBytes())
	}

	// TASK-222: the Silero VAD model of whisper.cpp, downloaded on its own (docs/refs/whisper.md, ADR-0031).
	sep := spec.SeparateFiles()
	if spec.VADModel() != "ggml-silero-v5.1.2.bin" || len(sep) != 1 || sep[0].Name() != spec.VADModel() ||
		sep[0].Size() != 885098 || sep[0].SHA256() != "29940d98d42b91fbd05ce489f3ecf7c72f0a42f027e4875919a28fb4c04ea2cf" ||
		sep[0].Source().String() != "https://huggingface.co/ggml-org/whisper-vad/resolve/main/ggml-silero-v5.1.2.bin" {
		t.Fatalf("VAD model %q, separate files %+v", spec.VADModel(), sep)
	}
}

// The verified set next to the executable activates speech recognition at start.
func TestSpeechReadyAtStartFromTheBundledFolder(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	core := startCore(t, a)
	if st := core.Speech.Status(); st.State != speechcomponent.StateReady {
		t.Fatalf("status %+v, want ready", st)
	}
	if err := core.Speech.StartInstall(); !errors.Is(err, speechcomponent.ErrAlreadyActive) {
		t.Fatalf("StartInstall = %v, want ErrAlreadyActive", err)
	}
	// The shutdown cancels a running install before it stops the rest (TestShutdownOrder).
	if a.life == nil || a.life.cancelSpeech == nil {
		t.Fatal("the lifecycle does not cancel the speech install at shutdown")
	}
}

// TASK-072: without the component the STT job waits; the install from the page downloads the archive from the
// allowed host, unpacks it into <data dir>/components/speech and the same job runs without a restart.
func TestSpeechComponentInstallsWithoutRestart(t *testing.T) {
	ctx := context.Background()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name, _, _ := hashTestBinary(t, bin)
	archive := zipOf(t, "Release/"+name, bin)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release/speech.zip" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)

	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	a.env.speech = speechEnv{
		manifest:   testComponentManifest(t, bin, srv.URL+"/release/speech.zip", len(archive), hex.EncodeToString(sum[:])),
		bundledDir: t.TempDir(), // nothing next to the executable
		source:     httpdownload.New(httpdownload.WithHTTPClient(srv.Client())),
	}
	core := startCore(t, a)
	if st := core.Speech.Status(); st.State != speechcomponent.StateMissing {
		t.Fatalf("status %+v, want missing", st)
	}

	model := installModel(t, a, "m1", testenv.WhisperOK)
	rec := completedRecording(t, a, "rec-1")
	if err := a.core.transcriptions.RecordingCompleted(ctx, rec); err != nil {
		t.Fatal(err)
	}
	jobs := sqlite.NewJobRepository(a.core.db)
	waiting := onlyJob(t, jobs, transcribe.JobType)
	if waiting.PinnedModelID() != model || waiting.Status() != job.StatusQueued {
		t.Fatalf("STT job %s pinned %s status %s", waiting.ID(), waiting.PinnedModelID(), waiting.Status())
	}

	if err := core.Speech.StartInstall(); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the component is installed", func() bool {
		st := core.Speech.Status()
		if st.State == speechcomponent.StateFailed {
			t.Fatalf("install failed: %v", st.Err)
		}
		return st.State == speechcomponent.StateReady
	}, nil)
	testenv.Eventually(t, "the waiting STT job runs", func() bool {
		j, err := jobs.Get(ctx, waiting.ID())
		return err == nil && j.Status() == job.StatusCompleted
	}, nil)

	installed := componentstore.InstalledDir(dir)
	if entries, err := os.ReadDir(installed); err != nil || len(entries) != 1 || entries[0].Name() != name {
		t.Fatalf("installed folder: %v, %v", entries, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(installed)); err != nil || len(entries) != 1 {
		t.Fatalf("components holds %v, %v; want only the installed folder (the archive is removed)", entries, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// The next start finds the installed set and activates it.
	b, _ := newTestAppWith(t, dir)
	b.env.speech = a.env.speech
	if st := startCore(t, b).Speech.Status(); st.State != speechcomponent.StateReady {
		t.Fatalf("after a restart: %+v, want ready", st)
	}
}

// TASK-222: the install also downloads the VAD model on its own, puts it next to the program, and the engine passes
// it to whisper-cli (the fake refuses --vad without an existing -vm file).
func TestSpeechComponentInstallsTheVADModel(t *testing.T) {
	ctx := context.Background()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name, size, binSum := hashTestBinary(t, bin)
	archive := zipOf(t, "Release/"+name, bin)
	vad := []byte("silero vad model of the test")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string][]byte{"/release/speech.zip": archive, "/vad/ggml-silero.bin": vad}[r.URL.Path]
		if body == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	sum, vadSum := sha256.Sum256(archive), sha256.Sum256(vad)
	manifest := fmt.Appendf(nil, `{"schema_version": 1, "version": "test", "source_url": %q, "size_bytes": %d,
 "sha256": %q, "download_hosts": [], "entry_prefix": "Release/", "executable": %q, "vad_model": "ggml-silero.bin",
 "files": [{"name": %q, "size_bytes": %d, "sha256": %q},
 {"name": "ggml-silero.bin", "size_bytes": %d, "sha256": %q, "source_url": %q}]}`,
		srv.URL+"/release/speech.zip", len(archive), hex.EncodeToString(sum[:]), name, name, size, binSum,
		len(vad), hex.EncodeToString(vadSum[:]), srv.URL+"/vad/ggml-silero.bin")

	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	a.env.speech = speechEnv{
		manifest: manifest, bundledDir: t.TempDir(), source: httpdownload.New(httpdownload.WithHTTPClient(srv.Client())),
	}
	core := startCore(t, a)
	installModel(t, a, "m1", testenv.WhisperOK)
	rec := completedRecording(t, a, "rec-1")
	if err := a.core.transcriptions.RecordingCompleted(ctx, rec); err != nil {
		t.Fatal(err)
	}

	if err := core.Speech.StartInstall(); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the component is installed", func() bool {
		st := core.Speech.Status()
		if st.State == speechcomponent.StateFailed {
			t.Fatalf("install failed: %v", st.Err)
		}
		return st.State == speechcomponent.StateReady
	}, nil)
	jobs := sqlite.NewJobRepository(a.core.db)
	stt := onlyJob(t, jobs, transcribe.JobType)
	testenv.Eventually(t, "the STT job runs with the VAD model", func() bool {
		j, err := jobs.Get(ctx, stt.ID())
		if err == nil && j.Status() == job.StatusFailed {
			t.Fatalf("STT job failed: %s", j.LastError())
		}
		return err == nil && j.Status() == job.StatusCompleted
	}, nil)

	installed := componentstore.InstalledDir(dir)
	entries, err := os.ReadDir(installed)
	if err != nil || len(entries) != 2 {
		t.Fatalf("installed folder: %v, %v; want the program and the VAD model", entries, err)
	}
	if got, err := os.ReadFile(filepath.Join(installed, "ggml-silero.bin")); err != nil || !bytes.Equal(got, vad) {
		t.Fatalf("installed VAD model %q, %v", got, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(installed)); err != nil || len(entries) != 1 {
		t.Fatalf("components holds %v, %v; want only the installed folder (the downloads are removed)", entries, err)
	}
}

// zipOf is an archive with the file at path stored under entry.
func zipOf(t *testing.T, entry, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.CreateHeader(&zip.FileHeader{Name: entry, Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
