package visitfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "subject-visits.json")
	return New(path), path
}

func TestMissingFileIsNeverOpened(t *testing.T) {
	s, _ := newStore(t)
	at, err := s.LastOpened(context.Background(), "s1")
	if err != nil || !at.IsZero() {
		t.Fatalf("LastOpened = %v, %v; want zero", at, err)
	}
}

func TestMarkThenRead(t *testing.T) {
	s, path := newStore(t)
	ctx := context.Background()
	if err := s.MarkOpened(ctx, "s1", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOpened(ctx, "s2", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOpened(ctx, "s1", t0.Add(2*time.Hour)); err != nil { // a later opening replaces the first
		t.Fatal(err)
	}

	// A new Store over the same file reads what the first wrote.
	again := New(path)
	for id, want := range map[shared.SubjectID]time.Time{"s1": t0.Add(2 * time.Hour), "s2": t0.Add(time.Hour), "s3": {}} {
		at, err := again.LastOpened(ctx, id)
		if err != nil || !at.Equal(want) {
			t.Errorf("LastOpened(%s) = %v, %v; want %v", id, at, err, want)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the file (no temporary left)", len(entries))
	}
}

func TestCorruptFile(t *testing.T) {
	for name, content := range map[string]string{
		"not json":   "{not json",
		"too large":  `{"subjects":{}}` + strings.Repeat(" ", maxBytes),
		"wrong type": `{"subjects":{"s1":"yesterday"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, path := newStore(t)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.LastOpened(context.Background(), "s1"); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("LastOpened = %v, want ErrCorrupt", err)
			}
			// The next opening replaces the damaged file.
			if err := s.MarkOpened(context.Background(), "s1", t0); err != nil {
				t.Fatal(err)
			}
			if at, err := s.LastOpened(context.Background(), "s1"); err != nil || !at.Equal(t0) {
				t.Fatalf("after repair LastOpened = %v, %v", at, err)
			}
		})
	}
}

func TestEmptyObjectIsNeverOpened(t *testing.T) {
	s, path := newStore(t)
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if at, err := s.LastOpened(context.Background(), "s1"); err != nil || !at.IsZero() {
		t.Fatalf("LastOpened = %v, %v", at, err)
	}
	if err := s.MarkOpened(context.Background(), "s1", t0); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRequests(t *testing.T) {
	s, path := newStore(t)
	ctx := context.Background()
	if _, err := s.LastOpened(ctx, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("LastOpened(\"\") = %v", err)
	}
	if err := s.MarkOpened(ctx, "", t0); !errors.Is(err, ErrInvalid) {
		t.Errorf("MarkOpened(\"\") = %v", err)
	}
	if err := s.MarkOpened(ctx, "s1", time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("MarkOpened(zero time) = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.MarkOpened(cancelled, "s1", t0); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkOpened(cancelled) = %v", err)
	}
	if _, err := s.LastOpened(cancelled, "s1"); !errors.Is(err, context.Canceled) {
		t.Errorf("LastOpened(cancelled) = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused request wrote the file: %v", err)
	}
}

func TestUnwritableDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	s := New(filepath.Join(dir, "subject-visits.json"))
	err := s.MarkOpened(context.Background(), "s1", t0)
	if !errors.Is(err, ErrIO) {
		t.Fatalf("MarkOpened = %v, want ErrIO", err)
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("error %q names the path", err)
	}
}

func TestTargetIsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subject-visits.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	_, err := s.LastOpened(context.Background(), "s1")
	if !errors.Is(err, ErrIO) {
		t.Fatalf("LastOpened = %v, want ErrIO", err)
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("error %q names the path", err)
	}
}

func TestOldestDroppedPastMax(t *testing.T) {
	s, path := newStore(t)
	ctx := context.Background()
	full := map[string]int64{}
	for i := range MaxEntries {
		full[fmt.Sprintf("s%04d", i)] = t0.Add(time.Duration(i) * time.Minute).UnixMilli()
	}
	data, err := json.Marshal(document{Subjects: full})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOpened(ctx, "s-new", t0.Add(-time.Hour)); err != nil { // the oldest itself
		t.Fatal(err)
	}
	if err := s.MarkOpened(ctx, "s-newer", t0.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for id, kept := range map[shared.SubjectID]bool{"s-new": false, "s0000": false, "s0001": true, "s-newer": true} {
		at, err := s.LastOpened(ctx, id)
		if err != nil || at.IsZero() == kept {
			t.Errorf("LastOpened(%s) = %v, %v; kept %v", id, at, err, kept)
		}
	}
}

func TestConcurrentOpenings(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.MarkOpened(ctx, shared.SubjectID(fmt.Sprintf("s%d", i)), t0); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := range 8 {
		if at, err := s.LastOpened(ctx, shared.SubjectID(fmt.Sprintf("s%d", i))); err != nil || !at.Equal(t0) {
			t.Errorf("s%d: %v, %v", i, at, err)
		}
	}
}
