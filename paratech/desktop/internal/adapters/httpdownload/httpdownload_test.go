package httpdownload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/transcription/values"
)

var content = bytes.Repeat([]byte("0123456789"), 1000) // 10000 bytes

type server struct {
	*httptest.Server
	hits atomic.Int64
}

// newServer starts an httptest TLS server; handler nil serves content with http.ServeContent (Range support).
func newServer(t *testing.T, handler http.HandlerFunc) *server {
	t.Helper()
	s := &server{}
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "m.bin", time.Time{}, bytes.NewReader(content))
		}
	}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) host() string { return strings.TrimPrefix(s.URL, "https://") }

func (s *server) src(t *testing.T, path string) values.SourceURL {
	t.Helper()
	u, err := values.ParseSourceURL(s.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func allowlist(t *testing.T, hosts ...string) values.HostAllowlist {
	t.Helper()
	a, err := values.NewHostAllowlist(hosts)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testClient(s *server, opts ...Option) *Client {
	return New(append([]Option{WithHTTPClient(s.Client())}, opts...)...)
}

func readAll(t *testing.T, rc io.ReadCloser) ([]byte, error) {
	t.Helper()
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("Close = %v", err)
		}
	}()
	return io.ReadAll(rc)
}

func TestFetchWholeAndRanged(t *testing.T) {
	var gotHeaders atomic.Value
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotHeaders.Store(r.Header.Clone())
		http.ServeContent(w, r, "m.bin", time.Time{}, bytes.NewReader(content))
	})
	c := testClient(s)
	allow := allowlist(t, s.host())

	reply, rc, err := c.Fetch(context.Background(), s.src(t, "/m.bin"), 0, allow)
	if err != nil {
		t.Fatal(err)
	}
	if reply != (values.RangeReply{Status: values.RangeFull, Total: int64(len(content))}) {
		t.Fatalf("reply = %+v", reply)
	}
	b, err := readAll(t, rc)
	if err != nil || !bytes.Equal(b, content) {
		t.Fatalf("body = %d bytes, %v", len(b), err)
	}
	h := gotHeaders.Load().(http.Header)
	if h.Get("Range") != "" || h.Get("Accept-Encoding") != "identity" || h.Get("User-Agent") != userAgent {
		t.Fatalf("headers of a whole request = %v", h)
	}

	reply, rc, err = c.Fetch(context.Background(), s.src(t, "/m.bin"), 4000, allow)
	if err != nil {
		t.Fatal(err)
	}
	if reply != (values.RangeReply{Status: values.RangePartial, Start: 4000, Total: int64(len(content))}) {
		t.Fatalf("ranged reply = %+v", reply)
	}
	b, err = readAll(t, rc)
	if err != nil || !bytes.Equal(b, content[4000:]) {
		t.Fatalf("ranged body = %d bytes, %v", len(b), err)
	}
	if got := gotHeaders.Load().(http.Header).Get("Range"); got != "bytes=4000-" {
		t.Fatalf("Range = %q", got)
	}

	reply, rc, err = c.Fetch(context.Background(), s.src(t, "/m.bin"), int64(len(content)), allow)
	if err != nil {
		t.Fatal(err)
	}
	if reply != (values.RangeReply{Status: values.RangeNotSatisfiable, Total: int64(len(content))}) {
		t.Fatalf("416 reply = %+v", reply)
	}
	if b, err := readAll(t, rc); err != nil || len(b) != 0 {
		t.Fatalf("416 body = %q, %v", b, err)
	}
}

func TestFetchReplies(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    values.RangeReply
		wantErr error
	}{
		{
			name: "server ignores the range",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "10000")
				_, _ = w.Write(content)
			},
			want: values.RangeReply{Status: values.RangeFull, Total: int64(len(content))},
		},
		{
			name: "unknown length",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.(http.Flusher).Flush() // chunked: no Content-Length
				_, _ = w.Write(content)
			},
			want: values.RangeReply{Status: values.RangeFull, Total: values.UnknownTotal},
		},
		{
			name:    "206 with unknown total",
			handler: partial("bytes 100-9999/*", content[100:]),
			want:    values.RangeReply{Status: values.RangePartial, Start: 100, Total: values.UnknownTotal},
		},
		{name: "206 without content range", handler: partial("", content[100:]), wantErr: ErrBadReply},
		{name: "206 unsatisfied form", handler: partial("bytes */10000", nil), wantErr: ErrBadReply},
		{name: "206 end before start", handler: partial("bytes 100-50/10000", nil), wantErr: ErrBadReply},
		{name: "206 end past total", handler: partial("bytes 100-10000/10000", content[100:]), wantErr: ErrBadReply},
		{name: "206 signed number", handler: partial("bytes +100-9999/10000", content[100:]), wantErr: ErrBadReply},
		{name: "206 other unit", handler: partial("items 100-9999/10000", content[100:]), wantErr: ErrBadReply},
		{name: "206 length differs", handler: partial("bytes 100-199/10000", content[100:]), wantErr: ErrBadReply},
		{
			name: "206 multipart",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "multipart/byteranges; boundary=x")
				w.Header().Set("Content-Range", "bytes 100-9999/10000")
				w.WriteHeader(http.StatusPartialContent)
			},
			wantErr: ErrBadReply,
		},
		{
			name: "416 with a bad content range",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Range", "bytes 0-1/10000")
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			},
			wantErr: ErrBadReply,
		},
		{
			name: "416 without content range",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			},
			want: values.RangeReply{Status: values.RangeNotSatisfiable, Total: values.UnknownTotal},
		},
		{
			name: "compressed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(content)
			},
			wantErr: ErrBadReply,
		},
		{
			name:    "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
			wantErr: ErrStatus,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, tc.handler)
			reply, rc, err := testClient(s).Fetch(context.Background(), s.src(t, "/m.bin"), 100, allowlist(t, s.host()))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || rc != nil {
					t.Fatalf("Fetch = %+v, %v; want %v", reply, err, tc.wantErr)
				}
				return
			}
			if err != nil || reply != tc.want {
				t.Fatalf("Fetch = %+v, %v; want %+v", reply, err, tc.want)
			}
			_ = rc.Close()
		})
	}

	s := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	_, _, err := testClient(s).Fetch(context.Background(), s.src(t, "/m.bin"), 0, allowlist(t, s.host()))
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusServiceUnavailable {
		t.Fatalf("Fetch on 503 = %v, want *StatusError 503", err)
	}
}

func partial(contentRange string, b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if contentRange != "" {
			w.Header().Set("Content-Range", contentRange)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(b)
	}
}

func TestFetchRedirects(t *testing.T) {
	other := newServer(t, nil) // allowlisted only in some cases; another port is another host
	var target atomic.Value
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Start 10 in the reply proves that the Range header survived the redirect.
		if r.URL.Path == "/m.bin" {
			http.ServeContent(w, r, "m.bin", time.Time{}, bytes.NewReader(content))
			return
		}
		http.Redirect(w, r, target.Load().(string), http.StatusFound)
	})

	tests := []struct {
		name    string
		target  string
		hosts   []string
		wantErr error
		offHits int64 // requests the other server may see
	}{
		{name: "to the same host", target: "/m.bin", hosts: []string{s.host()}},
		{name: "to an allowlisted host", target: other.URL + "/m.bin", hosts: []string{s.host(), other.host()}, offHits: 1},
		{name: "to a host outside the allowlist", target: other.URL + "/m.bin", hosts: []string{s.host()}, wantErr: values.ErrHostNotAllowed},
		{name: "to plain http", target: "http://" + other.host() + "/m.bin", hosts: []string{s.host(), other.host()}, wantErr: values.ErrInvalidSourceURL},
		{name: "loop", target: "/loop", hosts: []string{s.host()}, wantErr: ErrTooManyRedirects},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target.Store(tc.target)
			before := other.hits.Load()
			c := New(WithHTTPClient(s.Client()))
			reply, rc, err := c.Fetch(context.Background(), s.src(t, "/start"), 10, allowlist(t, tc.hosts...))
			if got := other.hits.Load() - before; got != tc.offHits {
				t.Fatalf("other server got %d requests, want %d", got, tc.offHits)
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Fetch = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || reply.Status != values.RangePartial || reply.Start != 10 {
				t.Fatalf("Fetch = %+v, %v", reply, err)
			}
			if b, err := readAll(t, rc); err != nil || !bytes.Equal(b, content[10:]) {
				t.Fatalf("body = %d bytes, %v", len(b), err)
			}
		})
	}
}

func TestFetchRefusesSourceOutsideAllowlist(t *testing.T) {
	s := newServer(t, nil)
	_, _, err := testClient(s).Fetch(context.Background(), s.src(t, "/m.bin"), 0, allowlist(t, "huggingface.co"))
	if !errors.Is(err, values.ErrHostNotAllowed) {
		t.Fatalf("Fetch = %v, want ErrHostNotAllowed", err)
	}
	if _, _, err := testClient(s).Fetch(context.Background(), s.src(t, "/m.bin"), -1, allowlist(t, s.host())); err == nil {
		t.Fatal("Fetch accepted a negative offset")
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("server got %d requests", n)
	}
}

// TestFetchEmptyAllowlistAllowsNothing: the allowlist of a catalog without entries (and the zero value) allows no
// host, so nothing is requested.
func TestFetchEmptyAllowlistAllowsNothing(t *testing.T) {
	s := newServer(t, nil)
	cat, err := values.ParseCatalog([]byte(`{"schema_version": 1, "models": []}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, allow := range map[string]values.HostAllowlist{
		"empty catalog": cat.DownloadHosts(), "empty list": allowlist(t), "zero value": {},
	} {
		_, _, err := testClient(s).Fetch(context.Background(), s.src(t, "/m.bin"), 0, allow)
		if !errors.Is(err, values.ErrHostNotAllowed) {
			t.Fatalf("%s: Fetch = %v, want ErrHostNotAllowed", name, err)
		}
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("server got %d requests", n)
	}
}

func TestDefaultTransportRefusesLocalAddresses(t *testing.T) {
	s := newServer(t, nil)
	_, _, err := New().Fetch(context.Background(), s.src(t, "/m.bin"), 0, allowlist(t, s.host()))
	if !errors.Is(err, ErrForbiddenAddress) || !errors.Is(err, ErrTransfer) {
		t.Fatalf("Fetch of a loopback server = %v, want ErrForbiddenAddress", err)
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("server got %d requests", n)
	}
}

func TestPublicAddressOnly(t *testing.T) {
	tests := []struct {
		addr string
		ok   bool
	}{
		{"8.8.8.8:443", true},
		{"[2606:4700::1111]:443", true},
		{"127.0.0.1:443", false},
		{"[::1]:443", false},
		{"10.1.2.3:443", false},
		{"172.16.0.1:443", false},
		{"192.168.1.1:443", false},
		{"169.254.169.254:80", false},
		{"[fe80::1%25eth0]:443", false},
		{"[fe80::1]:443", false},
		{"[fd00::1]:443", false},
		{"0.0.0.0:443", false},
		{"[::]:443", false},
		{"100.64.0.1:443", false},
		{"224.0.0.1:443", false},
		{"255.255.255.255:443", false},
		{"[::ffff:127.0.0.1]:443", false},
		{"[::ffff:10.0.0.1]:443", false},
		{"[64:ff9b::a00:1]:443", false},
		{"198.18.0.1:443", false},
		{"not-an-ip:443", false},
		{"8.8.8.8", false},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			err := publicAddressOnly("tcp", tc.addr, nil)
			if tc.ok != (err == nil) || (!tc.ok && !errors.Is(err, ErrForbiddenAddress)) {
				t.Fatalf("publicAddressOnly(%q) = %v, want ok=%v", tc.addr, err, tc.ok)
			}
		})
	}
	if !netip.MustParseAddr("8.8.8.8").IsGlobalUnicast() {
		t.Fatal("netip classification changed")
	}
}

// brokenHandler sends half of the file under a full Content-Length and drops the connection.
func brokenHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content[:5000])
		w.(http.Flusher).Flush()
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}
}

func TestFetchBodyBreaks(t *testing.T) {
	s := newServer(t, brokenHandler(t))
	_, rc, err := testClient(s).Fetch(context.Background(), s.src(t, "/m.bin"), 0, allowlist(t, s.host()))
	if err != nil {
		t.Fatal(err)
	}
	b, err := readAll(t, rc)
	if !errors.Is(err, ErrTransfer) || len(b) != 5000 {
		t.Fatalf("read = %d bytes, %v; want 5000 and ErrTransfer", len(b), err)
	}
}

func TestFetchStalls(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	t.Run("body", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10000")
			_, _ = w.Write(content[:100])
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		_, rc, err := testClient(s, WithIdleTimeout(150*time.Millisecond)).Fetch(context.Background(), s.src(t, "/m.bin"), 0, allowlist(t, s.host()))
		if err != nil {
			t.Fatal(err)
		}
		b, err := readAll(t, rc)
		if !errors.Is(err, ErrStalled) || len(b) != 100 {
			t.Fatalf("read = %d bytes, %v; want 100 and ErrStalled", len(b), err)
		}
	})

	t.Run("headers", func(t *testing.T) {
		s := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		_, _, err := testClient(s, WithIdleTimeout(150*time.Millisecond)).Fetch(context.Background(), s.src(t, "/m.bin"), 0, allowlist(t, s.host()))
		if !errors.Is(err, ErrStalled) {
			t.Fatalf("Fetch = %v, want ErrStalled", err)
		}
	})
}

func TestFetchCancel(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10000")
		_, _ = w.Write(content[:100])
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	_, rc, err := testClient(s).Fetch(ctx, s.src(t, "/m.bin"), 0, allowlist(t, s.host()))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 100)
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := rc.Read(buf); !errors.Is(err, context.Canceled) {
		t.Fatalf("read after cancel = %v, want context.Canceled", err)
	}
	_ = rc.Close()

	if _, _, err := testClient(s).Fetch(ctx, s.src(t, "/m.bin"), 0, allowlist(t, s.host())); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch with a cancelled context = %v", err)
	}
}
