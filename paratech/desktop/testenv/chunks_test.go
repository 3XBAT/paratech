package testenv

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

func TestChunkPayload(t *testing.T) {
	full := EncodeChunk([]byte("audio"))
	cases := []struct {
		name    string
		content []byte
		want    string
		ok      bool
	}{
		{"complete", full, "audio", true},
		{"empty payload", EncodeChunk(nil), "", true},
		{"cut payload", full[:len(full)-1], "", false},
		{"extra bytes", append(append([]byte(nil), full...), 'x'), "", false},
		{"cut header", full[:chunkHeaderLen-1], "", false},
		{"no header", []byte("audio"), "", false},
		{"wrong magic", append([]byte("XXXX"), full[4:]...), "", false},
		{"empty file", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ChunkPayload(tc.content)
			if ok != tc.ok || !bytes.Equal(got, []byte(tc.want)) {
				t.Fatalf("ChunkPayload = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// A chunk cut short by a crash is skipped; without a readable chunk Finalize reports recording.ErrNoAudio.
func TestFakeRecorderSkipsBrokenChunks(t *testing.T) {
	NoGoroutineLeaks(t)
	m, ctx := NewMemArtifacts(), context.Background()
	f := NewFakeRecorder(m, time.Minute)
	dir, out := ref(t, "recordings/r1"), ref(t, "recordings/r1.wav")

	if err := f.Start(ctx, "r1", dir, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.FeedTruncated(ctx, "r1", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Finalize(ctx, dir, out); !errors.Is(err, recording.ErrNoAudio) {
		t.Fatalf("Finalize of a broken chunk only = %v", err)
	}
	if _, err := m.Open(ctx, out); err == nil {
		t.Fatal("a track was written without audio")
	}

	for _, c := range []string{"a", "b"} {
		if _, err := f.Feed(ctx, "r1", []byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.FeedTruncated(ctx, "r1", []byte("c")); err != nil {
		t.Fatal(err)
	}
	d, err := f.Finalize(ctx, dir, out)
	if err != nil || d != 2*time.Minute || read(t, m, out) != "ab" {
		t.Fatalf("Finalize = %v, %v", d, err)
	}
	if paths := strings.Join(m.Paths(), " "); !strings.Contains(paths, "chunk-0004") {
		t.Fatalf("the broken chunk was removed: %s", paths)
	}
	if _, err := f.Finalize(ctx, ref(t, "recordings/none"), ref(t, "recordings/none.wav")); !errors.Is(err, recording.ErrNoAudio) {
		t.Fatalf("Finalize without chunks = %v", err)
	}
	if err := f.Stop(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
}
