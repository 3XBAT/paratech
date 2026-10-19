package testenv

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

func ref(t *testing.T, s string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.ParseArtifactRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func read(t *testing.T, m *MemArtifacts, r shared.ArtifactRef) string {
	t.Helper()
	rc, err := m.Open(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	return string(b)
}

func TestMemArtifacts(t *testing.T) {
	m, ctx := NewMemArtifacts(), context.Background()
	for _, p := range []string{"recordings/r1/chunk-0002", "recordings/r1/chunk-0001", "recordings/r1/sub/x", "recordings/r1.wav"} {
		if err := m.Write(ctx, ref(t, p), strings.NewReader(p)); err != nil {
			t.Fatal(err)
		}
	}

	list, err := m.List(ctx, ref(t, "recordings/r1"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range list {
		names = append(names, r.String())
	}
	if want := []string{"recordings/r1/chunk-0001", "recordings/r1/chunk-0002", "recordings/r1/sub"}; !slices.Equal(names, want) {
		t.Fatalf("List = %v, want %v", names, want)
	}
	if _, err := m.List(ctx, ref(t, "recordings/none")); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("List of a missing directory = %v", err)
	}
	if _, err := m.List(ctx, ref(t, "recordings/r1.wav")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("List of a file = %v", err)
	}
	if err := m.Write(ctx, ref(t, "recordings/r1"), strings.NewReader("x")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Write over a directory = %v", err)
	}
	if _, err := m.Open(ctx, ref(t, "recordings/none")); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Open missing = %v", err)
	}

	m.FailWrites(shared.ErrStorageFull)
	if err := m.Write(ctx, ref(t, "exports/a"), strings.NewReader("x")); !errors.Is(err, shared.ErrStorageFull) {
		t.Fatalf("failing Write = %v", err)
	}
	m.FailWrites(nil)

	if err := m.Delete(ctx, ref(t, "recordings/r1")); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, ref(t, "recordings/r1")); err != nil {
		t.Fatalf("repeated Delete = %v", err)
	}
	if got := m.Paths(); !slices.Equal(got, []string{"recordings/r1.wav"}) {
		t.Fatalf("after Delete: %v", got)
	}
}

func TestFakeRecorderCaptureAndFinalize(t *testing.T) {
	NoGoroutineLeaks(t)
	m, ctx := NewMemArtifacts(), context.Background()
	f := NewFakeRecorder(m, time.Minute)
	dir := ref(t, "recordings/r1")

	if _, err := f.Feed(ctx, "r1", []byte("a")); !errors.Is(err, ErrNotCapturing) {
		t.Fatalf("Feed before Start = %v", err)
	}
	if err := f.Start(ctx, "r1", dir, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if err := f.Start(ctx, "r1", dir, values.WholeComputer()); err == nil {
		t.Fatal("a second Start of a running capture succeeded")
	}
	for _, c := range []string{"a", "b"} {
		if _, err := f.Feed(ctx, "r1", []byte(c)); err != nil {
			t.Fatal(err)
		}
	}

	waited := make(chan error, 1)
	go func() { waited <- f.Wait(ctx, "r1") }()
	if err := f.Stop(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil || f.Running("r1") {
		t.Fatalf("Wait after Stop = %v, running %v", err, f.Running("r1"))
	}
	if err := f.Stop(ctx, "r1"); !errors.Is(err, ErrNotCapturing) {
		t.Fatalf("second Stop = %v", err)
	}

	out := ref(t, "recordings/r1.wav")
	d, err := f.Finalize(ctx, dir, out)
	if err != nil || d.Duration != 2*time.Minute || d.Silent() || read(t, m, out) != "ab" {
		t.Fatalf("Finalize = %v, %v", d, err)
	}
	if _, err := f.Finalize(ctx, ref(t, "recordings/empty"), ref(t, "recordings/empty.wav")); err == nil {
		t.Fatal("Finalize without chunks succeeded")
	}
	if f.Starts() != 2 || f.Finalizes() != 2 {
		t.Fatalf("calls: %d starts, %d finalizes", f.Starts(), f.Finalizes())
	}
}

func TestFakeRecorderFailures(t *testing.T) {
	NoGoroutineLeaks(t)
	m, ctx := NewMemArtifacts(), context.Background()
	f := NewFakeRecorder(m, time.Minute)
	boom := errors.New("boom")

	f.FailStart(boom)
	if err := f.Start(ctx, "r1", ref(t, "recordings/r1"), values.WholeComputer()); !errors.Is(err, boom) {
		t.Fatalf("Start = %v", err)
	}
	f.FailStart(nil)

	if err := f.Start(ctx, "r1", ref(t, "recordings/r1"), values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	m.FailWrites(shared.ErrStorageFull)
	if _, err := f.Feed(ctx, "r1", []byte("a")); !errors.Is(err, shared.ErrStorageFull) {
		t.Fatalf("Feed on a full disk = %v", err)
	}
	if err := f.Wait(ctx, "r1"); !errors.Is(err, shared.ErrStorageFull) || f.Running("r1") {
		t.Fatalf("Wait after a failed write = %v", err)
	}

	if err := f.Start(ctx, "r2", ref(t, "recordings/r2"), values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	f.End("r2", nil)
	if err := f.Wait(ctx, "r2"); err == nil {
		t.Fatal("a capture that ended by itself reported nil")
	}

	f.FailFinalize(boom)
	if _, err := f.Finalize(ctx, ref(t, "recordings/r1"), ref(t, "recordings/r1.wav")); !errors.Is(err, boom) {
		t.Fatalf("Finalize = %v", err)
	}
}

func TestFakeRecorderHolds(t *testing.T) {
	NoGoroutineLeaks(t)
	f := NewFakeRecorder(NewMemArtifacts(), time.Minute)
	entered, release := f.HoldStart()

	done := make(chan error, 1)
	go func() { done <- f.Start(context.Background(), "r1", ref(t, "recordings/r1"), values.WholeComputer()) }()
	<-entered
	release()
	release() // idempotent
	if err := <-done; err != nil || !f.Running("r1") {
		t.Fatalf("held Start = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	entered, release = f.HoldFinalize()
	defer release()
	go func() {
		_, err := f.Finalize(ctx, ref(t, "recordings/r1"), ref(t, "recordings/r1.wav"))
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("held Finalize after cancel = %v", err)
	}
	if err := f.Stop(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
}

func TestFakeRecorderKeepsTheSource(t *testing.T) {
	f := NewFakeRecorder(NewMemArtifacts(), time.Minute)
	if _, ok := f.Source("r1"); ok {
		t.Fatal("a source before any start")
	}
	zoom, err := values.SourceOf("zoom.exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Start(context.Background(), "r1", ref(t, "recordings/r1"), zoom); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.Source("r1"); !ok || got != zoom {
		t.Fatalf("Source = %v, %v; want %v", got, ok, zoom)
	}
}

// TASK-082: chunks of zeros make a silent track; Switch changes the source of a running capture.
func TestFakeRecorderSilentTrackAndSwitch(t *testing.T) {
	ctx := context.Background()
	m := NewMemArtifacts()
	f := NewFakeRecorder(m, time.Minute)
	dir := ref(t, "recordings/r1")
	zoom, err := values.SourceOf("zoom.exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Start(ctx, "r1", dir, zoom); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Feed(ctx, "r1", make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if err := f.Switch(ctx, "r1", values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if err := f.SetStats("r1", values.CaptureStats{Packets: 10, AudiblePackets: 4}); err != nil {
		t.Fatal(err)
	}
	d, ok := f.Diagnostics(ctx, "r1")
	if !ok || d.App != "" || d.TargetMode != values.CaptureExclude || d.SourceSwitches != 1 || d.Stats.AudiblePackets != 4 {
		t.Fatalf("diagnostics %+v", d)
	}
	if err := f.Stop(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Switch(ctx, "r1", zoom); !errors.Is(err, ErrNotCapturing) {
		t.Fatalf("Switch after the stop = %v", err)
	}
	tr, err := f.Finalize(ctx, dir, ref(t, "recordings/r1.wav"))
	if err != nil || !tr.Silent() || tr.Seconds != 60 || tr.AudibleSeconds != 0 {
		t.Fatalf("Finalize = %+v, %v; want a silent minute", tr, err)
	}
}
