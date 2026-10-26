//go:build windows

package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupported means this computer cannot print to PDF: no WebView2 Runtime or one older than 98.0.1108.43.
// The same values are declared in webview_other.go.
var ErrUnsupported = errors.New("pdf: webview2 runtime " + minRuntimeVersion + " or newer is required")

// ErrFailed means the runtime is present but this print did not produce a PDF.
var ErrFailed = errors.New("pdf: printing failed")

const (
	// maxHTMLBytes is the NavigateToString limit of WebView2.
	maxHTMLBytes = 2 << 20

	dirPerm = 0o700

	// Temporary results are named print-<random>.pdf (os.CreateTemp pattern).
	tempPattern = "print-*.pdf"
	tempPrefix  = "print-"
	tempSuffix  = ".pdf"
)

var pdfMagic = []byte("%PDF-")

// Operation names used in errors and logs.
const (
	opInput       = "input"
	opDataDir     = "data dir"
	opTempFile    = "temp file"
	opReadResult  = "read result"
	opCheckResult = "check result"
	opFindRuntime = "find runtime"
	opThread      = "thread"
	opWindow      = "window"
	opLoadRuntime = "load runtime"
	opEnvironment = "environment"
	opController  = "controller"
	opSettings    = "settings"
	opSubscribe   = "subscribe"
	opNavigate    = "navigate"
	opPrintSetup  = "print settings"
	opPrint       = "print"
)

// opError keeps the failing step and HRESULT for the log; callers only match err (ErrUnsupported or ErrFailed).
type opError struct {
	op     string
	hr     uintptr
	detail string
	err    error
}

func (e *opError) Error() string {
	msg := "pdf: " + e.op
	if e.hr != 0 {
		msg += fmt.Sprintf(": hresult 0x%08x", hresult(e.hr))
	}
	if e.detail != "" {
		msg += ": " + e.detail
	}
	return msg + ": " + e.err.Error()
}

func (e *opError) Unwrap() error { return e.err }

func failure(op string, hr uintptr, detail string) error {
	return &opError{op: op, hr: hr, detail: detail, err: ErrFailed}
}

// Printer turns print-ready HTML into PDF bytes with its own hidden WebView2 instance. It never touches the
// windows or the WebView2 profile of the app: dataDir is a separate user data folder and holds the temporary
// PDF. Calls are serialized; each one runs on its own OS thread and tears everything down before returning.
type Printer struct {
	dataDir string
	log     *slog.Logger
	sem     chan struct{}

	// enableScripts is set only by tests, to prove that the script check would notice a running script.
	enableScripts bool
}

// NewPrinter returns a Printer that keeps the WebView2 profile and temporary files under dataDir.
func NewPrinter(dataDir string, log *slog.Logger) *Printer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Printer{dataDir: dataDir, log: log, sem: make(chan struct{}, 1)}
}

// Print renders html (a complete UTF-8 page, scripts are not run) to PDF. ctx bounds the whole call, including
// the wait for an earlier call; on cancellation it returns ctx.Err() once the WebView2 objects are released,
// which may take up to settleTimeout when the cancellation hits a pending creation.
func (p *Printer) Print(ctx context.Context, html []byte) ([]byte, error) {
	if len(html) == 0 {
		return nil, failure(opInput, 0, "empty html")
	}
	if len(html) > maxHTMLBytes {
		return nil, failure(opInput, 0, "html larger than 2 MB")
	}
	if bytes.IndexByte(html, 0) >= 0 {
		return nil, failure(opInput, 0, "html contains NUL")
	}

	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.sem }()

	out, err := p.print(ctx, string(html))
	if err != nil {
		p.logFailure(err)
		return nil, err
	}
	return out, nil
}

func (p *Printer) print(ctx context.Context, html string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rt, err := findRuntime()
	if err != nil {
		return nil, err
	}

	// WebView2 resolves a relative user data folder against the executable, not the working directory.
	dataDir, err := filepath.Abs(p.dataDir)
	if err != nil {
		return nil, failure(opDataDir, 0, "")
	}
	if err := os.MkdirAll(dataDir, dirPerm); err != nil {
		return nil, failure(opDataDir, 0, "")
	}
	removeStaleResults(dataDir)

	tmp, err := os.CreateTemp(dataDir, tempPattern)
	if err != nil {
		return nil, failure(opTempFile, 0, "")
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }()
	if err := tmp.Close(); err != nil {
		return nil, failure(opTempFile, 0, "")
	}

	s := &session{
		runtime:       rt,
		userDataDir:   dataDir,
		html:          html,
		pdfPath:       path,
		enableScripts: p.enableScripts,
	}
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	if err := <-done; err != nil {
		return nil, err
	}

	out, err := os.ReadFile(path) //nolint:gosec // G304: the name came from os.CreateTemp in our data dir
	if err != nil {
		return nil, failure(opReadResult, 0, "")
	}
	if !bytes.HasPrefix(out, pdfMagic) {
		return nil, failure(opCheckResult, 0, "result is not a PDF")
	}
	return out, nil
}

// removeStaleResults deletes temporary PDFs of earlier calls: after a cancelled print the browser process may
// still finish writing its file once the call has removed it. Calls are serialized, so none is in use here.
func removeStaleResults(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(name, tempPrefix) && strings.HasSuffix(name, tempSuffix) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func (p *Printer) logFailure(err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		p.log.Info("pdf print stopped", "reason", err.Error())
		return
	}
	var oe *opError
	if errors.As(err, &oe) {
		p.log.Warn("pdf print failed", "op", oe.op, "hresult", fmt.Sprintf("0x%08x", hresult(oe.hr)),
			"detail", oe.detail, "unsupported", errors.Is(err, ErrUnsupported))
		return
	}
	p.log.Warn("pdf print failed", "err", err.Error())
}
