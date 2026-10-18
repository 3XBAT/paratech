package speechcomponent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

const plentyOfSpace = 1 << 40

type env struct {
	t         *testing.T
	spec      values.SpeechComponent
	files     *fakeFiles
	source    *fakeSource
	space     fakeSpace
	activator *fakeActivator
	svc       *Service
}

func newEnv(t *testing.T, mutate func(*env)) *env {
	t.Helper()
	testenv.NoGoroutineLeaks(t)
	e := &env{
		t: t, spec: testSpec(t), files: newFakeFiles(), source: newFakeSource(), space: fakeSpace{free: plentyOfSpace},
		activator: &fakeActivator{},
	}
	if mutate != nil {
		mutate(e)
	}
	svc, err := New(Deps{Spec: e.spec, Source: e.source, Space: e.space, Files: e.files, Activator: e.activator})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return e
}

// installed runs an install to its end and returns the final status.
func (e *env) installed() Status {
	e.t.Helper()
	e.svc.MarkReady()
	if err := e.svc.StartInstall(); err != nil {
		e.t.Fatalf("StartInstall: %v", err)
	}
	return e.settled()
}

// settled waits until no install runs.
func (e *env) settled() Status {
	e.t.Helper()
	var st Status
	testenv.Eventually(e.t, "the install to end", func() bool {
		st = e.svc.Status()
		return st.State != StateInstalling
	}, nil)
	return st
}

func TestNewRequiresEveryPort(t *testing.T) {
	spec := testSpec(t)
	full := Deps{Spec: spec, Source: newFakeSource(), Space: fakeSpace{}, Files: newFakeFiles(), Activator: &fakeActivator{}}
	for name, d := range map[string]Deps{
		"no spec":      {Source: full.Source, Space: full.Space, Files: full.Files, Activator: full.Activator},
		"no source":    {Spec: spec, Space: full.Space, Files: full.Files, Activator: full.Activator},
		"no space":     {Spec: spec, Source: full.Source, Files: full.Files, Activator: full.Activator},
		"no files":     {Spec: spec, Source: full.Source, Space: full.Space, Activator: full.Activator},
		"no activator": {Spec: spec, Source: full.Source, Space: full.Space, Files: full.Files},
	} {
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: New = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name      string
		bundled   []values.FoundFile
		installed []values.FoundFile
		inspect   error
		activate  error
		want      State
		wantErr   error
		activated []values.ComponentFolder
	}{
		{name: "nothing anywhere", want: StateMissing},
		{name: "bundled", bundled: verifiedSet(), installed: verifiedSet(), want: StateReady,
			activated: []values.ComponentFolder{values.FolderBundled}},
		{name: "installed after a damaged bundle", bundled: damagedSet(), installed: verifiedSet(), want: StateReady,
			activated: []values.ComponentFolder{values.FolderInstalled}},
		{name: "damaged bundle", bundled: damagedSet(), want: StateDamaged},
		{name: "damaged install", installed: verifiedSet()[:1], want: StateDamaged},
		{name: "unreadable folder", inspect: errors.New("access denied"), want: StateDamaged, wantErr: errors.New("")},
		{name: "activation fails", bundled: verifiedSet(), activate: errors.New("no engine"), want: StateFailed,
			wantErr: ErrActivationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, func(e *env) {
				e.files.folders[values.FolderBundled] = tt.bundled
				e.files.folders[values.FolderInstalled] = tt.installed
				e.files.inspectErr[values.FolderBundled] = tt.inspect
				e.activator.err = tt.activate
			})
			st, err := e.svc.Detect(context.Background())
			if (err != nil) != (tt.wantErr != nil) || (tt.wantErr != nil && tt.wantErr.Error() != "" && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("Detect error = %v, want %v", err, tt.wantErr)
			}
			if st.State != tt.want {
				t.Fatalf("state = %s, want %s", st.State, tt.want)
			}
			if got := e.activator.activated(); !slices.Equal(got, tt.activated) {
				t.Fatalf("activated %v, want %v", got, tt.activated)
			}
			if tt.want == StateFailed && !errors.Is(st.Err, ErrActivationFailed) {
				t.Fatalf("status error = %v, want ErrActivationFailed", st.Err)
			}
		})
	}
}

func TestInstallActivatesWithoutRestart(t *testing.T) {
	e := newEnv(t, nil)
	if st, _ := e.svc.Detect(context.Background()); st.State != StateMissing {
		t.Fatalf("before: %s", st.State)
	}

	if st := e.installed(); st.State != StateReady {
		t.Fatalf("after the install: %+v", st)
	}
	if got := e.activator.activated(); !slices.Equal(got, []values.ComponentFolder{values.FolderInstalled}) {
		t.Fatalf("activated %v, want the installed folder", got)
	}
	if unpacked, discarded := e.files.counts(); unpacked != 1 || discarded != 1 {
		t.Fatalf("unpacked %d, discarded %d; want 1 and 1", unpacked, discarded)
	}
	if !slices.Equal(e.source.offsets, []int64{0}) {
		t.Fatalf("fetched from %v, want the whole file once", e.source.offsets)
	}
	if got, want := e.source.allow.Hosts(), e.spec.DownloadHosts().Hosts(); !slices.Equal(got, want) {
		t.Fatalf("allowed hosts %v, want the manifest's %v", got, want)
	}
	if err := e.svc.StartInstall(); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("a second install = %v, want ErrAlreadyActive", err)
	}
}

func TestInstallFailures(t *testing.T) {
	short := archiveBytes[:len(archiveBytes)-1]
	longer := append(append([]byte(nil), archiveBytes...), 'x')
	other := bytes.Repeat([]byte("zap!"), len(archiveBytes)/4)
	body := func(b []byte) func(context.Context) io.ReadCloser {
		return func(context.Context) io.ReadCloser { return io.NopCloser(bytes.NewReader(b)) }
	}
	tests := []struct {
		name       string
		mutate     func(e *env)
		want       error
		wantUnpack bool
	}{
		{"no connection", func(e *env) { e.source.err = errors.New("dial: connection refused") }, ErrDownloadFailed, false},
		{"host not allowed", func(e *env) { e.source.err = values.ErrHostNotAllowed }, values.ErrHostNotAllowed, false},
		{"partial reply", func(e *env) {
			e.source.reply = values.RangeReply{Status: values.RangePartial, Total: int64(len(archiveBytes))}
		}, values.ErrUnexpectedRange, false},
		{"other length", func(e *env) {
			e.source.reply = values.RangeReply{Status: values.RangeFull, Total: int64(len(archiveBytes)) + 1}
		}, values.ErrSizeMismatch, false},
		{"ends early", func(e *env) { e.source.body = body(short) }, ErrDownloadFailed, false},
		{"longer", func(e *env) { e.source.body = body(longer) }, values.ErrSizeMismatch, false},
		{"other content", func(e *env) { e.source.body = body(other) }, values.ErrChecksumMismatch, false},
		{"broken transfer", func(e *env) {
			e.source.body = func(context.Context) io.ReadCloser { return &failingBody{part: archiveBytes[:10]} }
		}, ErrDownloadFailed, false},
		{"no disk space", func(e *env) { e.space.free = 1 }, values.ErrInsufficientSpace, false},
		{"disk space unknown", func(e *env) { e.space.err = errors.New("no volume") }, nil, false},
		{"disk fails", func(e *env) { e.files.saveErr = errors.New("disk failed") }, nil, false},
		{"archive without the files", func(e *env) { e.files.unpackErr = values.ErrInvalidComponentArchive },
			values.ErrInvalidComponentArchive, true},
		{"installed set does not verify", func(e *env) { e.files.unpackSet = damagedSet() }, ErrNotVerified, true},
		{"activation fails", func(e *env) { e.activator.err = errors.New("runner stopped") }, ErrActivationFailed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.mutate)
			st := e.installed()
			if st.State != StateFailed || st.Err == nil || (tt.want != nil && !errors.Is(st.Err, tt.want)) {
				t.Fatalf("status %+v, want failed with %v", st, tt.want)
			}
			unpacked, discarded := e.files.counts()
			if (unpacked > 0) != tt.wantUnpack {
				t.Fatalf("unpacked %d times, want %v: nothing is opened before the archive verifies", unpacked, tt.wantUnpack)
			}
			if discarded != 1 {
				t.Fatalf("archive discarded %d times, want 1", discarded)
			}

		})
	}
}

// Verified files that did not start stay reported as an activation failure, even after a later install fails
// otherwise: a reinstall does not fix them.
func TestActivationFailureWinsOverALaterInstallFailure(t *testing.T) {
	e := newEnv(t, func(e *env) {
		e.files.folders[values.FolderBundled] = verifiedSet()
		e.activator.err = errors.New("work dir not representable")
	})
	if st, _ := e.svc.Detect(context.Background()); st.State != StateFailed || !errors.Is(st.Err, ErrActivationFailed) {
		t.Fatalf("after Detect: %+v", st)
	}
	e.source.mu.Lock()
	e.source.err = errors.New("offline")
	e.source.mu.Unlock()
	if st := e.installed(); st.State != StateFailed || !errors.Is(st.Err, ErrActivationFailed) {
		t.Fatalf("after a failed install: %+v, want ErrActivationFailed", st)
	}
}

// A failed install can be started again.
func TestRetryAfterAFailure(t *testing.T) {
	e := newEnv(t, func(e *env) { e.source.err = errors.New("offline") })
	if st := e.installed(); st.State != StateFailed || !errors.Is(st.Err, ErrDownloadFailed) {
		t.Fatalf("first install: %+v", st)
	}
	e.source.mu.Lock()
	e.source.err = nil
	e.source.mu.Unlock()
	if st := e.installed(); st.State != StateReady {
		t.Fatalf("second install: %+v", st)
	}
}

func TestCancelInstall(t *testing.T) {
	entered := make(chan struct{})
	e := newEnv(t, func(e *env) {
		e.source.entered = entered
		e.source.body = func(ctx context.Context) io.ReadCloser { return &blockingBody{ctx: ctx, part: archiveBytes[:100]} }
		e.files.folders[values.FolderBundled] = damagedSet()
	})
	if _, err := e.svc.Detect(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.svc.MarkReady()
	if err := e.svc.StartInstall(); err != nil {
		t.Fatal(err)
	}
	<-entered
	testenv.Eventually(t, "download progress", func() bool {
		st := e.svc.Status()
		return st.State == StateInstalling && st.Stage == StageDownloading && st.DoneBytes == 100 &&
			st.TotalBytes == int64(len(archiveBytes))
	}, nil)
	if err := e.svc.StartInstall(); !errors.Is(err, ErrInstallInProgress) {
		t.Fatalf("second StartInstall = %v, want ErrInstallInProgress", err)
	}

	e.svc.CancelInstall()
	if st := e.settled(); st.State != StateDamaged || st.Err != nil {
		t.Fatalf("after cancel: %+v, want the damaged state without an error", st)
	}
	if unpacked, discarded := e.files.counts(); unpacked != 0 || discarded != 1 {
		t.Fatalf("unpacked %d, discarded %d; want 0 and 1", unpacked, discarded)
	}
}

// No install writes before the startup cleanups of the component folder.
func TestInstallWaitsForTheStartupCleanups(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.svc.StartInstall(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := e.source.callCount(); n != 0 {
		t.Fatalf("fetched %d times before MarkReady", n)
	}
	e.svc.MarkReady()
	if st := e.settled(); st.State != StateReady {
		t.Fatalf("status %+v, want ready", st)
	}
}

func TestCloseStopsTheInstall(t *testing.T) {
	entered := make(chan struct{})
	e := newEnv(t, func(e *env) {
		e.source.entered = entered
		e.source.body = func(ctx context.Context) io.ReadCloser { return &blockingBody{ctx: ctx, part: archiveBytes[:1]} }
	})
	e.svc.MarkReady()
	if err := e.svc.StartInstall(); err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.svc.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.StartInstall(); !errors.Is(err, ErrClosed) {
		t.Fatalf("StartInstall after Close = %v, want ErrClosed", err)
	}
	if st := e.svc.Status(); st.State != StateFailed {
		t.Fatalf("status after Close = %+v, want the interrupted install failed", st)
	}
}
