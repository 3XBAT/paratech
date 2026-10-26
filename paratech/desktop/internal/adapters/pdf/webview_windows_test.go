//go:build windows

package pdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// printTimeout is generous: a print takes about 1 s on a desktop, but the first start of the browser on a busy
	// machine can take far longer.
	printTimeout       = 2 * time.Minute
	profileReleaseWait = 20 * time.Second
)

// envWebView2CI=1 runs the real-WebView2 tests on GitHub Actions too. They are off there by default: on the hosted
// Windows runner the browser start is unpredictably slow (over a minute) and its processes starve the packages that
// run alongside. Locally they always run.
const envWebView2CI = "LA_WEBVIEW2_CI_TEST"

func realWebView2Off(getenv func(string) string) bool {
	return getenv("GITHUB_ACTIONS") == "true" && getenv(envWebView2CI) != "1"
}

const cyrillicPage = `<!DOCTYPE html><html lang="ru"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<style>@page { size: A4; margin: 20mm 10mm 20mm 30mm; } body { font-family: "Times New Roman"; font-size: 14pt; }</style>
</head><body><h1>Реферат</h1><p>Привет, мир! Проверка кириллицы в PDF.</p></body></html>`

// scriptPage would grow to five pages if its script ran: the script appends page-broken blocks.
const scriptPage = `<!DOCTYPE html><html><head><meta charset="utf-8"></head><body><p>Одна страница.</p>
<script>for (var i = 0; i < 4; i++) { var d = document.createElement('div');
d.style.breakBefore = 'page'; d.textContent = 'script ' + i; document.body.appendChild(d); }</script>
</body></html>`

// newTestPrinter keeps the WebView2 profile in a fresh temp dir. Not t.TempDir: the browser process exits a
// moment after the last release and holds profile files until then, so removal is retried for a while.
func newTestPrinter(t *testing.T) (*Printer, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "paratech-pdf-test-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(profileReleaseWait)
		for {
			err := os.RemoveAll(root)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("remove temp dir: %v", err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	dir := filepath.Join(root, "webview-pdf")
	return NewPrinter(dir, nil), dir
}

// requireRuntime skips tests that need a real WebView2 Runtime when the machine has none or they are off on CI.
func requireRuntime(t *testing.T) {
	t.Helper()
	if realWebView2Off(os.Getenv) {
		t.Skipf("real WebView2 tests are off on GitHub Actions; set %s=1 to run them", envWebView2CI)
	}
	if _, err := findRuntime(); err != nil {
		t.Skipf("no usable WebView2 runtime: %v", err)
	}
}

func printCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), printTimeout)
	t.Cleanup(cancel)
	return ctx
}

var pageCount = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)`)

// pages reads the page count from the page tree; Chromium writes that dictionary uncompressed.
func pages(t *testing.T, pdf []byte) int {
	t.Helper()
	m := pageCount.FindSubmatch(pdf)
	if m == nil {
		t.Fatalf("no page tree with /Count in the PDF")
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("page count %q: %v", m[1], err)
	}
	return n
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(dir, "print-*.pdf"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(left) > 0 {
		t.Fatalf("temporary PDFs left behind: %v", left)
	}
}

func TestPrintRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		html []byte
	}{
		{name: "empty", html: nil},
		{name: "over 2 MB", html: bytes.Repeat([]byte("a"), maxHTMLBytes+1)},
		{name: "NUL byte", html: []byte("<p>a\x00b</p>")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, dir := newTestPrinter(t)

			out, err := p.Print(t.Context(), tt.html)

			if !errors.Is(err, ErrFailed) || out != nil {
				t.Fatalf("Print = %d bytes, %v; want ErrFailed", len(out), err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("data dir touched before validation: %v", err)
			}
		})
	}
}

func TestPrintCancelledBeforeStart(t *testing.T) {
	p, dir := newTestPrinter(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := p.Print(ctx, []byte(cyrillicPage))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Print err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("data dir touched by a cancelled call: %v", err)
	}
}

func TestPrintWaitsForEarlierCallWithinContext(t *testing.T) {
	p, _ := newTestPrinter(t)
	p.sem <- struct{}{} // an earlier call is still running
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Print(ctx, []byte(cyrillicPage))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Print err = %v, want context.DeadlineExceeded", err)
	}
}

func TestVersionCheck(t *testing.T) {
	minVersion, ok := parseVersion(minRuntimeVersion)
	if !ok {
		t.Fatalf("minRuntimeVersion %q does not parse", minRuntimeVersion)
	}
	tests := []struct {
		version string
		valid   bool
		atLeast bool
	}{
		{version: "98.0.1108.43", valid: true, atLeast: true},
		{version: "98.0.1108.42", valid: true, atLeast: false},
		{version: "97.9.9999.99", valid: true, atLeast: false},
		{version: "155.0.4283.62", valid: true, atLeast: true},
		{version: "98.0.1108", valid: false},
		{version: "98.0.x.43", valid: false},
		{version: "Application", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			v, ok := parseVersion(tt.version)
			if ok != tt.valid {
				t.Fatalf("parseVersion ok = %v, want %v", ok, tt.valid)
			}
			if ok && (compareVersions(v, minVersion) >= 0) != tt.atLeast {
				t.Fatalf("version %s at least %s = %v, want %v", tt.version, minRuntimeVersion, !tt.atLeast, tt.atLeast)
			}
		})
	}
}

func TestPrintCyrillicPage(t *testing.T) {
	requireRuntime(t)
	p, dir := newTestPrinter(t)

	out, err := p.Print(printCtx(t), []byte(cyrillicPage))

	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) || len(out) < 100 {
		t.Fatalf("result is not a PDF: %d bytes, starts with %q", len(out), out[:min(len(out), 8)])
	}
	if n := pages(t, out); n != 1 {
		t.Fatalf("pages = %d, want 1", n)
	}
	assertNoTempFiles(t, dir)
}

func TestPrintTwiceInARow(t *testing.T) {
	requireRuntime(t)
	p, dir := newTestPrinter(t)

	for i := range 2 {
		out, err := p.Print(printCtx(t), []byte(cyrillicPage))
		if err != nil {
			t.Fatalf("Print #%d: %v", i+1, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF-")) {
			t.Fatalf("Print #%d: not a PDF", i+1)
		}
	}
	assertNoTempFiles(t, dir)
}

func TestPrintConcurrentCallsAreSerialized(t *testing.T) {
	requireRuntime(t)
	p, dir := newTestPrinter(t)
	ctx := printCtx(t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			out, err := p.Print(ctx, []byte(cyrillicPage))
			if err == nil && !bytes.HasPrefix(out, []byte("%PDF-")) {
				err = errors.New("not a PDF")
			}
			errs[i] = err
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Print #%d: %v", i+1, err)
		}
	}
	assertNoTempFiles(t, dir)
}

// TestPrintTimeoutThenRecovers cancels at several points (environment, controller, page, print). Each call
// must end with the deadline error or, if it was fast enough, a PDF; the next call must work and the profile
// must become removable, which proves the browser process was released.
func TestPrintTimeoutThenRecovers(t *testing.T) {
	requireRuntime(t)
	for _, deadline := range []time.Duration{30 * time.Millisecond, 200 * time.Millisecond,
		400 * time.Millisecond, 700 * time.Millisecond} {
		t.Run(deadline.String(), func(t *testing.T) {
			p, dir := newTestPrinter(t)

			ctx, cancel := context.WithTimeout(t.Context(), deadline)
			defer cancel()
			start := time.Now()
			out, err := p.Print(ctx, []byte(cyrillicPage))
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Print with a short deadline: err = %v, want context.DeadlineExceeded", err)
			}
			if err == nil && !bytes.HasPrefix(out, []byte("%PDF-")) {
				t.Fatalf("Print with a short deadline: no error and not a PDF")
			}
			if took := time.Since(start); took > deadline+settleTimeout+2*time.Second {
				t.Fatalf("cancelled Print took %v", took)
			}
			assertNoTempFiles(t, dir)

			out, err = p.Print(printCtx(t), []byte(cyrillicPage))
			if err != nil {
				t.Fatalf("Print after a timeout: %v", err)
			}
			if !bytes.HasPrefix(out, []byte("%PDF-")) {
				t.Fatalf("Print after a timeout: not a PDF")
			}
		})
	}
}

// TestPrintDoesNotRunScripts checks the page count: scriptPage has one page unless its script runs. The control
// run enables scripts through the test-only switch to prove the check would notice a running script.
func TestPrintDoesNotRunScripts(t *testing.T) {
	requireRuntime(t)
	tests := []struct {
		name    string
		scripts bool
		want    int
	}{
		{name: "scripts off (production)", scripts: false, want: 1},
		{name: "control: scripts on", scripts: true, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPrinter(t)
			p.enableScripts = tt.scripts

			out, err := p.Print(printCtx(t), []byte(scriptPage))

			if err != nil {
				t.Fatalf("Print: %v", err)
			}
			if n := pages(t, out); n != tt.want {
				t.Fatalf("pages = %d, want %d", n, tt.want)
			}
		})
	}
}

// fivePages is a static document of five pages. Single quotes only: it is also embedded in a double-quoted
// attribute.
const fivePages = `<p>1</p><div style='break-before:page'>2</div><div style='break-before:page'>3</div>` +
	`<div style='break-before:page'>4</div><div style='break-before:page'>5</div>`

var fontObject = regexp.MustCompile(`/Type\s*/Font\b`)

// TestPrintCancelsOtherNavigations holds the loaded page before printing so that a later navigation would have
// happened. Both checks fail when the NavigationStarting cancels are disabled. A refresh to a data: URL would not
// do: Chromium blocks top-level data: navigations itself; and an iframe cannot change the outer page count, so
// the iframe case checks that its text (the only text there) never reached the PDF.
func TestPrintCancelsOtherNavigations(t *testing.T) {
	requireRuntime(t)
	tests := []struct {
		name      string
		html      string
		wantPages int
		wantFonts bool
	}{
		{
			name: "meta refresh to a one-page document",
			html: `<!DOCTYPE html><html><head><meta charset="utf-8">` +
				`<meta http-equiv="refresh" content="0;url=about:blank"></head>` +
				`<body>` + fivePages + `</body></html>`,
			wantPages: 5,
			wantFonts: true,
		},
		{
			name: "iframe with a data: document",
			html: `<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>` +
				`<iframe src="data:text/html,` + fivePages + `"></iframe></body></html>`,
			wantPages: 1,
			wantFonts: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPrinter(t)
			p.holdBeforePrint = 2 * time.Second

			out, err := p.Print(printCtx(t), []byte(tt.html))

			if err != nil {
				t.Fatalf("Print: %v", err)
			}
			if n := pages(t, out); n != tt.wantPages {
				t.Fatalf("pages = %d, want %d", n, tt.wantPages)
			}
			if fonts := fontObject.Match(out); fonts != tt.wantFonts {
				t.Fatalf("PDF has fonts = %v, want %v", fonts, tt.wantFonts)
			}
		})
	}
}

func TestRealWebView2OffOnlyOnCIWithoutOptIn(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		off  bool
	}{
		{name: "local", env: nil, off: false},
		{name: "local with the opt-in", env: map[string]string{envWebView2CI: "1"}, off: false},
		{name: "GitHub Actions", env: map[string]string{"GITHUB_ACTIONS": "true"}, off: true},
		{name: "GitHub Actions with the opt-in", env: map[string]string{"GITHUB_ACTIONS": "true", envWebView2CI: "1"}, off: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := realWebView2Off(func(k string) string { return tt.env[k] }); got != tt.off {
				t.Fatalf("off = %v, want %v", got, tt.off)
			}
		})
	}
}

func TestRemoveStaleResultsOnlyOurTempFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]bool{ // name → removed
		"print-123.pdf":   true,
		"print-notes.txt": false,
		"report.pdf":      false,
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "print-dir.pdf"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	removeStaleResults(dir)

	for name, removed := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		if removed != os.IsNotExist(err) {
			t.Fatalf("%s: removed = %v, want %v", name, os.IsNotExist(err), removed)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "print-dir.pdf")); err != nil {
		t.Fatalf("directory with a matching name was touched: %v", err)
	}
}

func TestOpErrorKeepsKind(t *testing.T) {
	err := failure(opPrint, 0x80004005, "not successful")
	if !errors.Is(err, ErrFailed) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("failure() kind is wrong: %v", err)
	}
	if !strings.Contains(err.Error(), "0x80004005") || !strings.Contains(err.Error(), opPrint) {
		t.Fatalf("error text lacks step or hresult: %q", err.Error())
	}

	// Garbage above the low 32 bits of a register is not an HRESULT.
	high := ^uintptr(0) &^ math.MaxUint32
	if msg := failure(opPrint, high, "").Error(); strings.Contains(msg, "hresult") {
		t.Fatalf("zero HRESULT printed: %q", msg)
	}
}
