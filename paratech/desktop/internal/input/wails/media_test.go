package wails

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/transcriptview"
	"paratech/desktop/internal/domain/shared"
)

// fakeAudio serves one recording "r1" with data; opened counts opens, closed closes.
type fakeAudio struct {
	data   []byte
	err    error
	ids    []shared.RecordingID
	opened int
	closed int
}

type countingReader struct {
	*bytes.Reader
	a *fakeAudio
}

func (c countingReader) Close() error { c.a.closed++; return nil }

func (a *fakeAudio) OpenAudio(_ context.Context, id shared.RecordingID) (transcriptview.Audio, error) {
	a.ids = append(a.ids, id)
	if a.err != nil {
		return transcriptview.Audio{}, a.err
	}
	if id != "r1" {
		return transcriptview.Audio{}, transcriptview.ErrRecordingNotFound
	}
	a.opened++
	return transcriptview.Audio{Reader: countingReader{bytes.NewReader(a.data), a}, Size: int64(len(a.data))}, nil
}

func newMediaFixture(data []byte) (*mediaHandler, *fakeAudio, *int) {
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		_, _ = io.WriteString(w, "asset")
	})
	h := newMediaHandler(next, slog.New(slog.DiscardHandler))
	a := &fakeAudio{data: data}
	h.setSource(a)
	return h, a, &nextCalls
}

func serve(h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMediaServesRanges(t *testing.T) {
	data := []byte("RIFF0123456789abcdef")
	h, a, _ := newMediaFixture(data)

	tests := []struct {
		name, rng    string
		code         int
		body         string
		contentRange string
	}{
		{name: "whole small file", code: http.StatusOK, body: string(data)},
		{name: "open range", rng: "bytes=4-", code: http.StatusPartialContent, body: string(data[4:]), contentRange: "bytes 4-19/20"},
		{name: "closed range", rng: "bytes=4-7", code: http.StatusPartialContent, body: "0123", contentRange: "bytes 4-7/20"},
		{name: "suffix", rng: "bytes=-3", code: http.StatusPartialContent, body: "def", contentRange: "bytes 17-19/20"},
		{name: "end past the size", rng: "bytes=18-100", code: http.StatusPartialContent, body: "ef", contentRange: "bytes 18-19/20"},
		{name: "first of several ranges", rng: "bytes=0-1, 5-6", code: http.StatusPartialContent, body: "RI", contentRange: "bytes 0-1/20"},
		{name: "start past the end", rng: "bytes=20-", code: http.StatusRequestedRangeNotSatisfiable, contentRange: "bytes */20"},
		{name: "garbage", rng: "items=1-2", code: http.StatusRequestedRangeNotSatisfiable, contentRange: "bytes */20"},
		{name: "reversed", rng: "bytes=5-2", code: http.StatusRequestedRangeNotSatisfiable, contentRange: "bytes */20"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(h, http.MethodGet, "/media/recording/r1", map[string]string{"Range": tc.rng})
			if rec.Code != tc.code {
				t.Fatalf("code = %d, want %d", rec.Code, tc.code)
			}
			if tc.body != "" && rec.Body.String() != tc.body {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.body)
			}
			if got := rec.Header().Get("Content-Range"); got != tc.contentRange {
				t.Fatalf("Content-Range = %q, want %q", got, tc.contentRange)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("headers = %v", rec.Header())
			}
			if tc.code < 300 && rec.Header().Get("Content-Type") != "audio/wav" {
				t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
			}
		})
	}
	if a.opened != a.closed {
		t.Fatalf("opened %d readers, closed %d", a.opened, a.closed)
	}
}

// A large recording is never sent whole: Wails on Windows buffers a response in memory.
func TestMediaCapsTheResponse(t *testing.T) {
	data := bytes.Repeat([]byte("x"), maxMediaChunk*2+10)
	h, _, _ := newMediaFixture(data)

	for _, rng := range []string{"", "bytes=0-", "bytes=100-"} {
		rec := serve(h, http.MethodGet, "/media/recording/r1", map[string]string{"Range": rng})
		if rec.Code != http.StatusPartialContent || rec.Body.Len() != maxMediaChunk {
			t.Fatalf("Range %q: %d with %d bytes, want 206 with %d", rng, rec.Code, rec.Body.Len(), maxMediaChunk)
		}
		if got, _ := strconv.Atoi(rec.Header().Get("Content-Length")); got != maxMediaChunk {
			t.Fatalf("Range %q: Content-Length %d", rng, got)
		}
	}
	rec := serve(h, http.MethodGet, "/media/recording/r1", map[string]string{"Range": "bytes=-5"})
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 5 {
		t.Fatalf("suffix of a large file: %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestMediaHead(t *testing.T) {
	h, _, _ := newMediaFixture([]byte("RIFF0123"))
	rec := serve(h, http.MethodHead, "/media/recording/r1", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "8" ||
		rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("HEAD = %d, %d bytes, headers %v", rec.Code, rec.Body.Len(), rec.Header())
	}
}

// Conditional headers never produce a 304: WebView2 hangs on it.
func TestMediaIgnoresValidators(t *testing.T) {
	h, _, _ := newMediaFixture([]byte("RIFF0123"))
	rec := serve(h, http.MethodGet, "/media/recording/r1", map[string]string{
		"If-Modified-Since": "Mon, 01 Jan 2035 00:00:00 GMT", "If-None-Match": "*", "If-Range": `"x"`, "Range": "bytes=0-1",
	})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "RI" {
		t.Fatalf("with validators: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Last-Modified") != "" || rec.Header().Get("ETag") != "" {
		t.Fatalf("validators sent: %v", rec.Header())
	}
}

func TestMediaRefusals(t *testing.T) {
	h, a, next := newMediaFixture([]byte("RIFF"))
	tests := []struct {
		name, method, target string
		code                 int
	}{
		{"unknown recording", http.MethodGet, "/media/recording/r2", http.StatusNotFound},
		{"no id", http.MethodGet, "/media/recording/", http.StatusNotFound},
		{"nested path", http.MethodGet, "/media/recording/r1/x", http.StatusNotFound},
		{"encoded separator", http.MethodGet, "/media/recording/..%2Fsecret", http.StatusNotFound},
		{"backslash", http.MethodGet, "/media/recording/..%5Csecret", http.StatusNotFound},
		{"dots", http.MethodGet, "/media/recording/..", http.StatusNotFound},
		{"post", http.MethodPost, "/media/recording/r1", http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(h, tc.method, tc.target, nil)
			if rec.Code != tc.code {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.target, rec.Code, tc.code)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("an error response may be cached")
			}
		})
	}
	for _, id := range a.ids {
		if strings.ContainsAny(string(id), `/\`) {
			t.Fatalf("an id with a separator reached the use case: %q", id)
		}
	}
	if *next != 0 {
		t.Fatalf("media paths fell through to the assets %d times", *next)
	}

	// Storage failures are a 500 without details; incomplete recordings a 404.
	a.err = errors.Join(transcriptview.ErrUnavailable, errors.New(`C:\Users\student`))
	if rec := serve(h, http.MethodGet, "/media/recording/r1", nil); rec.Code != http.StatusInternalServerError ||
		strings.Contains(rec.Body.String(), "student") {
		t.Fatalf("storage failure = %d %q", rec.Code, rec.Body.String())
	}
	a.err = transcriptview.ErrNoAudio
	if rec := serve(h, http.MethodGet, "/media/recording/r1", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("no audio = %d", rec.Code)
	}
}

func TestMediaFallsThroughAndWaitsForTheCore(t *testing.T) {
	h, _, next := newMediaFixture(nil)
	for _, p := range []string{"/", "/index.html", "/media/other/r1", "/media/recordingr1", "/assets/app.js"} {
		if rec := serve(h, http.MethodGet, p, nil); rec.Body.String() != "asset" {
			t.Fatalf("GET %s = %q, want the asset handler", p, rec.Body.String())
		}
	}
	if *next != 5 {
		t.Fatalf("asset handler called %d times", *next)
	}

	// Before the core is open the recordings are not found.
	early := newMediaHandler(http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	if rec := serve(early, http.MethodGet, "/media/recording/r1", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("before setSource = %d", rec.Code)
	}
}

func TestClampRange(t *testing.T) {
	const size = 10 * maxMediaChunk
	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{"", "bytes=0-" + strconv.Itoa(maxMediaChunk-1), true},
		{"bytes=0-", "bytes=0-" + strconv.Itoa(maxMediaChunk-1), true},
		{"bytes=5-9", "bytes=5-9", true},
		{" bytes= 5 - 9 ", "bytes=5-9", true},
		{"bytes=5 - 9", "bytes=5-9", true},
		{"bytes=-10", "bytes=" + strconv.Itoa(size-10) + "-" + strconv.Itoa(size-1), true},
		{"bytes=-" + strconv.Itoa(size+5), "bytes=0-" + strconv.Itoa(maxMediaChunk-1), true},
		{"bytes=-0", "", false},
		{"bytes=x-", "", false},
		{"bytes=-", "", false},
		{"bytes=5", "", false},
		{"bytes=99999999999999999999-", "", false},
		{"bytes=" + strconv.Itoa(size) + "-", "", false},
	}
	for _, tc := range tests {
		got, ok := clampRange(tc.header, size)
		if got != tc.want || ok != tc.ok {
			t.Errorf("clampRange(%q) = %q, %v; want %q, %v", tc.header, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := clampRange("", 100); got != "" || !ok {
		t.Fatalf("small file without a range = %q, %v", got, ok)
	}
	if _, ok := clampRange("bytes=0-", 0); ok {
		t.Fatal("a range of an empty file is satisfiable")
	}
}
