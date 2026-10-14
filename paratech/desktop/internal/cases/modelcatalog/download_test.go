package modelcatalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

var errReset = errors.New("connection reset by peer")

// serveFn answers one Fetch; call counts from 1.
type serveFn func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error)

type fetchCall struct {
	url    string
	offset int64
	hosts  []string
}

// fakeSource serves the catalog files of the tests by source URL.
type fakeSource struct {
	mu      sync.Mutex
	files   map[string][]byte
	serve   serveFn
	calls   []fetchCall
	onFetch func(call int)
}

func newFakeSource(models ...catalogModel) *fakeSource {
	s := &fakeSource{files: map[string][]byte{}, serve: honorRange}
	for _, m := range models {
		s.files[m.url()] = m.content()
	}
	return s
}

func (c catalogModel) url() string { return "https://example.com/" + c.id + ".bin" }

func (s *fakeSource) Fetch(ctx context.Context, src values.SourceURL, offset int64, allow values.HostAllowlist) (values.RangeReply, io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return values.RangeReply{}, nil, err
	}

	s.mu.Lock()
	s.calls = append(s.calls, fetchCall{url: src.String(), offset: offset, hosts: allow.Hosts()})
	call, content, serve, hook := len(s.calls), s.files[src.String()], s.serve, s.onFetch
	s.mu.Unlock()

	if hook != nil {
		hook(call)
	}
	if err := allow.Check(src.String()); err != nil {
		return values.RangeReply{}, nil, err
	}
	return serve(call, offset, content)
}

func (s *fakeSource) offsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int64, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.offset)
	}
	return out
}

func (s *fakeSource) setServe(fn serveFn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serve = fn
}

// honorRange is a correct server: 206 from the offset, 200 without one.
func honorRange(_ int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
	total := int64(len(content))
	if offset == 0 {
		return values.RangeReply{Status: values.RangeFull, Total: total}, body(content), nil
	}
	if offset >= total {
		return values.RangeReply{Status: values.RangeNotSatisfiable, Total: total}, body(nil), nil
	}
	return values.RangeReply{Status: values.RangePartial, Start: offset, Total: total}, body(content[offset:]), nil
}

// cutAt behaves like honorRange but drops the connection when the file reaches byte at, on the first call only.
func cutAt(at int64) serveFn {
	return func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
		reply, rc, err := honorRange(call, offset, content)
		if call != 1 || err != nil {
			return reply, rc, err
		}
		_ = rc.Close()
		return reply, io.NopCloser(io.MultiReader(bytes.NewReader(content[offset:at]), errorReader{errReset})), nil
	}
}

func body(b []byte) io.ReadCloser { return io.NopCloser(bytes.NewReader(b)) }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type fakeSpace struct {
	mu   sync.Mutex
	free int64
	err  error
}

func (f *fakeSpace) FreeBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.free, f.err
}

type downloadEnv struct {
	*env
	src   *fakeSource
	space *fakeSpace
	dl    *Downloader
}

func newDownloadEnv(t *testing.T) *downloadEnv {
	t.Helper()
	e := newEnv(t)
	src := newFakeSource(catTiny, catBase, catLarge)
	space := &fakeSpace{free: 1 << 40}
	dl, err := NewDownloader(e.svc, DownloadDeps{Source: src, Space: space})
	if err != nil {
		t.Fatal(err)
	}
	return &downloadEnv{env: e, src: src, space: space, dl: dl}
}

func (e *downloadEnv) partial(c catalogModel) shared.ArtifactRef {
	e.t.Helper()
	return modelRef(e.t, "downloads/"+c.id+"-"+string(c.sum())[:16]+".partial")
}

func (e *downloadEnv) fileBytes(ref shared.ArtifactRef) ([]byte, bool) {
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	b, ok := e.w.files[ref]
	return append([]byte(nil), b...), ok
}

func (e *downloadEnv) fileCount() int {
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	return len(e.w.files)
}

// requireNotInstalled checks that nothing is installed and that no model file is left anywhere.
func (e *downloadEnv) requireNotInstalled() {
	e.t.Helper()
	installed, err := e.svc.ListInstalled(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	if len(installed) != 0 {
		e.t.Fatalf("installed = %+v, want none", installed)
	}
	if n := e.fileCount(); n != 0 {
		e.t.Fatalf("%d files left, want none", n)
	}
}

func (e *downloadEnv) requireInstalled(m InstalledModel, c catalogModel) {
	e.t.Helper()
	installed, err := e.svc.ListInstalled(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	if len(installed) != 1 || installed[0].ID != m.ID || installed[0].CatalogID != values.CatalogID(c.id) ||
		installed[0].SHA256 != c.sum() {
		e.t.Fatalf("installed = %+v, want %s", installed, c.id)
	}
	if _, ok := e.fileBytes(e.partial(c)); ok {
		e.t.Fatal("the partial file is still there")
	}
}

func TestDownloadInstallsVerifiedModel(t *testing.T) {
	e := newDownloadEnv(t)
	events, unsubscribe := e.dl.Subscribe()
	defer unsubscribe()

	m, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
	if err != nil {
		t.Fatalf("Download = %v", err)
	}
	e.requireInstalled(m, catBase)

	b, ok := e.fileBytes(modelRef(t, "base-q5_1-m1.bin"))
	if !ok || !bytes.Equal(b, catBase.content()) {
		t.Fatalf("installed file = %v, %v", b, ok)
	}

	select {
	case p := <-events:
		want := DownloadProgress{CatalogID: values.CatalogID(catBase.id), Phase: PhaseInstalled, DoneBytes: 20, TotalBytes: 20}
		if p != want {
			t.Fatalf("last event = %+v, want %+v", p, want)
		}
	default:
		t.Fatal("no progress event")
	}

	wantHosts := []string{"example.com"}
	if got := e.src.calls[0].hosts; !reflect.DeepEqual(got, wantHosts) {
		t.Fatalf("Fetch allowlist = %v, want %v", got, wantHosts)
	}

	avail, err := e.svc.ListAvailable(context.Background(), values.Machine{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range avail {
		if a.Entry.ID() == values.CatalogID(catBase.id) && a.InstalledID != m.ID {
			t.Fatalf("ListAvailable does not show the download as installed: %+v", a)
		}
	}
}

func TestDownloadProgressEvents(t *testing.T) {
	e := newDownloadEnv(t)
	e.src.setServe(func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
		reply, _, err := honorRange(call, offset, content)
		return reply, io.NopCloser(iotestOneByte(content[offset:])), err
	})

	var mu sync.Mutex
	var got []DownloadProgress
	events, unsubscribe := e.dl.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for p := range events {
			mu.Lock()
			got = append(got, p)
			mu.Unlock()
		}
	}()

	if _, err := e.dl.Download(context.Background(), values.CatalogID(catTiny.id)); err != nil {
		t.Fatal(err)
	}
	unsubscribe()
	unsubscribe() // idempotent
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 || got[len(got)-1].Phase != PhaseInstalled {
		t.Fatalf("events = %+v, want PhaseInstalled last", got)
	}
	var last int64
	for _, p := range got {
		if p.TotalBytes != 10 || p.DoneBytes < last || p.DoneBytes > 10 {
			t.Fatalf("events = %+v: not monotonic within the size", got)
		}
		last = p.DoneBytes
	}
}

// iotestOneByte returns a reader that yields one byte per Read.
func iotestOneByte(b []byte) io.Reader { return &oneByte{b: b} }

type oneByte struct{ b []byte }

func (o *oneByte) Read(p []byte) (int, error) {
	if len(o.b) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0], o.b = o.b[0], o.b[1:]
	return 1, nil
}

func TestSlowSubscriberDoesNotStallDownload(t *testing.T) {
	e := newDownloadEnv(t)
	events, unsubscribe := e.dl.Subscribe() // never read until the end
	defer unsubscribe()
	e.src.setServe(func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
		reply, _, err := honorRange(call, offset, content)
		return reply, io.NopCloser(iotestOneByte(content[offset:])), err
	})

	done := make(chan error, 1)
	go func() {
		_, err := e.dl.Download(context.Background(), values.CatalogID(catLarge.id))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a subscriber that does not read stalled the download")
	}
	if p := <-events; p.Phase != PhaseInstalled {
		t.Fatalf("pending event = %+v, want the last one", p)
	}
}

func TestDownloadResumesAfterBrokenConnection(t *testing.T) {
	e := newDownloadEnv(t)
	e.src.setServe(cutAt(10))
	id := values.CatalogID(catBase.id)

	_, err := e.dl.Download(context.Background(), id)
	if !errors.Is(err, ErrDownloadInterrupted) || !errors.Is(err, errReset) {
		t.Fatalf("first Download = %v, want ErrDownloadInterrupted with the transport error", err)
	}
	if b, ok := e.fileBytes(e.partial(catBase)); !ok || len(b) != 10 {
		t.Fatalf("partial file = %d bytes (%v), want 10 kept", len(b), ok)
	}
	plan, err := e.dl.Preflight(context.Background(), id)
	if err != nil || plan.HaveBytes != 10 || plan.TotalBytes != 20 || plan.RequiredBytes != 10+values.DiskSpaceMargin {
		t.Fatalf("Preflight = %+v, %v", plan, err)
	}

	m, err := e.dl.Download(context.Background(), id)
	if err != nil {
		t.Fatalf("resumed Download = %v", err)
	}
	if got := e.src.offsets(); !reflect.DeepEqual(got, []int64{0, 10}) {
		t.Fatalf("requested offsets = %v, want [0 10]", got)
	}
	e.requireInstalled(m, catBase)
}

func TestDownloadResumeReplies(t *testing.T) {
	tests := []struct {
		name        string
		serve       serveFn
		wantOffsets []int64
		wantErr     error
		keepsBytes  int64 // with wantErr: bytes of the partial file left, -1: none
	}{
		{
			name: "server ignores the range",
			serve: func(_ int, _ int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{Status: values.RangeFull, Total: int64(len(content))}, body(content), nil
			},
			wantOffsets: []int64{8},
		},
		{
			name: "416 restarts from zero",
			serve: func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
				if call == 1 {
					return values.RangeReply{Status: values.RangeNotSatisfiable, Total: values.UnknownTotal}, body(nil), nil
				}
				return honorRange(call, offset, content)
			},
			wantOffsets: []int64{8, 0},
		},
		{
			name: "range from another start restarts",
			serve: func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
				if call == 1 {
					return values.RangeReply{Status: values.RangePartial, Start: 4, Total: 20}, body(content[4:]), nil
				}
				return honorRange(call, offset, content)
			},
			wantOffsets: []int64{8, 0},
		},
		{
			name: "source has another size",
			serve: func(_ int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{Status: values.RangePartial, Start: offset, Total: 21}, body(content[offset:]), nil
			},
			wantOffsets: []int64{8}, wantErr: values.ErrSizeMismatch, keepsBytes: -1,
		},
		{
			name: "source sends more than the size",
			serve: func(_ int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{Status: values.RangeFull, Total: values.UnknownTotal}, body(append(content, 'x')), nil
			},
			wantOffsets: []int64{8}, wantErr: values.ErrSizeMismatch, keepsBytes: -1,
		},
		{
			name: "source ends early",
			serve: func(_ int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{Status: values.RangePartial, Start: offset, Total: values.UnknownTotal}, body(content[offset:15]), nil
			},
			wantOffsets: []int64{8}, wantErr: ErrDownloadInterrupted, keepsBytes: 15,
		},
		{
			name: "416 twice",
			serve: func(int, int64, []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{Status: values.RangeNotSatisfiable, Total: values.UnknownTotal}, body(nil), nil
			},
			wantOffsets: []int64{8, 0}, wantErr: values.ErrUnexpectedRange, keepsBytes: -1,
		},
		{
			name: "transport error",
			serve: func(int, int64, []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{}, nil, errReset
			},
			wantOffsets: []int64{8}, wantErr: ErrDownloadInterrupted, keepsBytes: 8,
		},
		{
			name: "redirect to a host outside the allowlist",
			serve: func(int, int64, []byte) (values.RangeReply, io.ReadCloser, error) {
				return values.RangeReply{}, nil, fmt.Errorf("redirect: %w", values.ErrHostNotAllowed)
			},
			wantOffsets: []int64{8}, wantErr: values.ErrHostNotAllowed, keepsBytes: 8,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newDownloadEnv(t)
			e.putBytes(e.partial(catBase).Path(), catBase.content()[:8])
			e.src.setServe(tc.serve)

			m, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
			if got := e.src.offsets(); !reflect.DeepEqual(got, tc.wantOffsets) {
				t.Fatalf("requested offsets = %v, want %v", got, tc.wantOffsets)
			}
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Download = %v", err)
				}
				e.requireInstalled(m, catBase)
				return
			}

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Download = %v, want %v", err, tc.wantErr)
			}
			b, ok := e.fileBytes(e.partial(catBase))
			if tc.keepsBytes < 0 && ok || tc.keepsBytes >= 0 && int64(len(b)) != tc.keepsBytes {
				t.Fatalf("partial file = %d bytes (%v), want %d", len(b), ok, tc.keepsBytes)
			}
			if installed, _ := e.svc.ListInstalled(context.Background()); len(installed) != 0 {
				t.Fatalf("installed = %+v after a failed download", installed)
			}
		})
	}
}

func TestDownloadRejectsTamperedFile(t *testing.T) {
	e := newDownloadEnv(t)
	tampered := catBase.content()
	tampered[5] = 'X'
	e.src.files[catBase.url()] = tampered

	events, unsubscribe := e.dl.Subscribe()
	defer unsubscribe()

	_, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
	if !errors.Is(err, values.ErrChecksumMismatch) {
		t.Fatalf("Download = %v, want ErrChecksumMismatch", err)
	}
	e.requireNotInstalled()
	if p := <-events; p.Phase != PhaseFailed {
		t.Fatalf("last event = %+v, want PhaseFailed", p)
	}

	// The next download starts from byte 0 and installs the real file.
	e.src.files[catBase.url()] = catBase.content()
	m, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.src.offsets(); !reflect.DeepEqual(got, []int64{0, 0}) {
		t.Fatalf("requested offsets = %v, want [0 0]", got)
	}
	e.requireInstalled(m, catBase)
}

func TestDownloadPartialFileStates(t *testing.T) {
	t.Run("complete partial is verified without a request", func(t *testing.T) {
		e := newDownloadEnv(t)
		e.putBytes(e.partial(catBase).Path(), catBase.content())
		m, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
		if err != nil {
			t.Fatal(err)
		}
		if n := len(e.src.offsets()); n != 0 {
			t.Fatalf("%d requests, want none", n)
		}
		e.requireInstalled(m, catBase)
	})

	t.Run("partial longer than the model is discarded", func(t *testing.T) {
		e := newDownloadEnv(t)
		e.putBytes(e.partial(catBase).Path(), make([]byte, 30))
		m, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
		if err != nil {
			t.Fatal(err)
		}
		if got := e.src.offsets(); !reflect.DeepEqual(got, []int64{0}) {
			t.Fatalf("requested offsets = %v, want [0]", got)
		}
		e.requireInstalled(m, catBase)
	})

	t.Run("discard partial", func(t *testing.T) {
		e := newDownloadEnv(t)
		e.putBytes(e.partial(catBase).Path(), catBase.content()[:5])
		if err := e.dl.DiscardPartial(context.Background(), values.CatalogID(catBase.id)); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.fileBytes(e.partial(catBase)); ok {
			t.Fatal("partial file kept")
		}
		if err := e.dl.DiscardPartial(context.Background(), "nope"); !errors.Is(err, ErrNotInCatalog) {
			t.Fatalf("DiscardPartial of an unknown model = %v", err)
		}
	})
}

func TestDownloadChecksDiskSpaceFirst(t *testing.T) {
	e := newDownloadEnv(t)
	id := values.CatalogID(catBase.id)
	e.space.free = 19 + values.DiskSpaceMargin

	plan, err := e.dl.Preflight(context.Background(), id)
	want := DownloadPlan{
		CatalogID: id, TotalBytes: 20, RequiredBytes: 20 + values.DiskSpaceMargin,
		AvailableBytes: 19 + values.DiskSpaceMargin,
	}
	if err != nil || plan != want {
		t.Fatalf("Preflight = %+v, %v; want %+v", plan, err, want)
	}

	_, err = e.dl.Download(context.Background(), id)
	var ise *values.InsufficientSpaceError
	if !errors.As(err, &ise) || ise.Required != 20+values.DiskSpaceMargin || ise.Available != 19+values.DiskSpaceMargin {
		t.Fatalf("Download = %v, want *InsufficientSpaceError with the numbers", err)
	}
	if n := len(e.src.offsets()); n != 0 {
		t.Fatalf("%d requests made without disk space", n)
	}

	// Bytes kept from an earlier attempt lower the requirement.
	e.putBytes(e.partial(catBase).Path(), catBase.content()[:2])
	if plan, err := e.dl.Preflight(context.Background(), id); err != nil || !plan.Enough || plan.HaveBytes != 2 {
		t.Fatalf("Preflight with a partial file = %+v, %v", plan, err)
	}
	if _, err := e.dl.Download(context.Background(), id); err != nil {
		t.Fatalf("Download with enough space for the rest = %v", err)
	}

	e2 := newDownloadEnv(t)
	boom := errors.New("volume gone")
	e2.space.err = boom
	if _, err := e2.dl.Download(context.Background(), id); !errors.Is(err, boom) {
		t.Fatalf("Download without a disk space answer = %v", err)
	}
	if _, err := e2.dl.Preflight(context.Background(), id); !errors.Is(err, boom) {
		t.Fatalf("Preflight without a disk space answer = %v", err)
	}
}

func TestDownloadRefusals(t *testing.T) {
	t.Run("already installed", func(t *testing.T) {
		e := newDownloadEnv(t)
		e.install(catBase)
		if _, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id)); !errors.Is(err, ErrAlreadyInstalled) {
			t.Fatalf("Download = %v, want ErrAlreadyInstalled", err)
		}
		if n := len(e.src.offsets()); n != 0 {
			t.Fatalf("%d requests for an installed model", n)
		}
	})

	t.Run("not in catalog", func(t *testing.T) {
		e := newDownloadEnv(t)
		if _, err := e.dl.Download(context.Background(), "nope"); !errors.Is(err, ErrNotInCatalog) {
			t.Fatalf("Download = %v, want ErrNotInCatalog", err)
		}
		if _, err := e.dl.Preflight(context.Background(), "nope"); !errors.Is(err, ErrNotInCatalog) {
			t.Fatalf("Preflight = %v, want ErrNotInCatalog", err)
		}
	})

	t.Run("installed meanwhile", func(t *testing.T) {
		e := newDownloadEnv(t)
		e.src.onFetch = func(int) { e.install(catBase) }
		_, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
		if !errors.Is(err, ErrAlreadyInstalled) {
			t.Fatalf("Download = %v, want ErrAlreadyInstalled", err)
		}
		// Only the file of the other install is left.
		if n := e.fileCount(); n != 1 {
			t.Fatalf("%d files left, want 1", n)
		}
	})

	t.Run("disk fails", func(t *testing.T) {
		e := newDownloadEnv(t)
		e.w.appendErr = shared.ErrStorageFull
		_, err := e.dl.Download(context.Background(), values.CatalogID(catBase.id))
		if !errors.Is(err, shared.ErrStorageFull) || errors.Is(err, ErrDownloadInterrupted) {
			t.Fatalf("Download = %v, want ErrStorageFull", err)
		}
	})

	t.Run("bad config", func(t *testing.T) {
		e := newDownloadEnv(t)
		for _, d := range []DownloadDeps{{Source: e.src}, {Space: e.space}} {
			if _, err := NewDownloader(e.svc, d); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("NewDownloader(%+v) = %v", d, err)
			}
		}
		if _, err := NewDownloader(nil, DownloadDeps{Source: e.src, Space: e.space}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewDownloader(nil) = %v", err)
		}
	})
}

func TestDownloadOneAtATime(t *testing.T) {
	e := newDownloadEnv(t)
	entered := make(chan struct{})
	unblock := make(chan struct{})
	e.src.setServe(func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
		close(entered)
		<-unblock
		return honorRange(call, offset, content)
	})

	id := values.CatalogID(catBase.id)
	done := make(chan error, 1)
	go func() {
		_, err := e.dl.Download(context.Background(), id)
		done <- err
	}()
	<-entered

	if _, err := e.dl.Download(context.Background(), id); !errors.Is(err, ErrDownloadInProgress) {
		t.Errorf("second Download = %v, want ErrDownloadInProgress", err)
	}
	if err := e.dl.DiscardPartial(context.Background(), id); !errors.Is(err, ErrDownloadInProgress) {
		t.Errorf("DiscardPartial while downloading = %v, want ErrDownloadInProgress", err)
	}
	close(unblock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDownloadCancelKeepsPartial(t *testing.T) {
	e := newDownloadEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.src.setServe(func(call int, offset int64, content []byte) (values.RangeReply, io.ReadCloser, error) {
		reply, _, err := honorRange(call, offset, content)
		r := io.MultiReader(bytes.NewReader(content[:12]), readerFunc(func([]byte) (int, error) {
			cancel()
			return 0, nil
		}), bytes.NewReader(content[12:]))
		return reply, io.NopCloser(r), err
	})

	_, err := e.dl.Download(ctx, values.CatalogID(catBase.id))
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrDownloadInterrupted) {
		t.Fatalf("Download = %v, want context.Canceled", err)
	}
	if b, ok := e.fileBytes(e.partial(catBase)); !ok || len(b) != 12 {
		t.Fatalf("partial = %d bytes (%v), want 12 kept for resume", len(b), ok)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
