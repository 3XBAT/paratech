package winocr

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"strings"
	"time"
)

// Errors of the adapter (ARCH-32): every failure is one of them or wraps one, so callers use errors.Is.
var (
	// ErrUnsupported means Windows OCR is not available: another OS, or a Windows without the OCR classes.
	ErrUnsupported = errors.New("winocr: Windows OCR is not available")

	// ErrNoLanguage means Windows has no Russian recognizer; the message tells the user how to add it.
	ErrNoLanguage = errors.New("winocr: no Russian OCR recognizer installed; in Windows Settings > Time & language > " +
		"Language add Russian (Русский), install its optional feature \"Optical character recognition\", then restart the app")

	// ErrBadImage means the image is not a PNG or JPEG the standard library can decode (truncated, damaged, empty).
	ErrBadImage = errors.New("winocr: image cannot be decoded")

	// ErrTooLarge means the image is over material.MaxImageBytes or material.MaxImagePixels; it is not decoded.
	ErrTooLarge = errors.New("winocr: image too large")

	// ErrTimeout means one page took longer than the page time limit.
	ErrTimeout = errors.New("winocr: page recognition timed out")

	// ErrEngine means the OCR engine or WinRT failed (see HResultError) or panicked.
	ErrEngine = errors.New("winocr: OCR engine failed")
)

// HResultError is a failed WinRT call. It matches ErrEngine with errors.Is.
type HResultError struct {
	Op   string
	Code uint32
}

func (e *HResultError) Error() string {
	return fmt.Sprintf("winocr: %s failed with HRESULT 0x%08X", e.Op, e.Code)
}

// Is makes every HRESULT failure an ErrEngine.
func (e *HResultError) Is(target error) bool { return target == ErrEngine }

// Language is the only recognizer language (ADR-0027): never another one, never the user-profile languages.
const Language = "ru"

// DefaultPageTimeout bounds the recognition of one page; a printed page takes about a second.
const DefaultPageTimeout = time.Minute

// enginePrefix starts the engine version string.
const enginePrefix = "windows-ocr"

// Line is one recognised line: its text and its box in pixels of the input image.
type Line struct {
	Text string
	Box  image.Rectangle
}

// Page is the result of one image.
type Page struct {
	Lines []Line

	// Engine identifies what recognised the page, as "windows-ocr/<recognizer language>/<Windows major.minor.build>",
	// e.g. "windows-ocr/ru/10.0.19045". A Windows update of the OCR language pack within one build is not reflected.
	Engine string
}

// rectF mirrors Windows.Foundation.Rect (four float32: X, Y, Width, Height).
type rectF struct {
	X, Y, Width, Height float32
}

// rawLine is a line as the engine returns it: text and the boxes of its words, in pixels of the bitmap it saw.
type rawLine struct {
	text  string
	words []rectF
}

// platform is the OCR engine of the OS; nil off Windows. The prepare callback decodes the image only once the engine
// exists and reports its maximum dimension.
type platform interface {
	languages(ctx context.Context) ([]string, error)
	recognize(ctx context.Context, lang string, prepare func(maxDim int) (*image.Gray, error)) (lines []rawLine, tag string, err error)
	osVersion() string
}

// Reader recognises pages with Windows OCR. Create it with New; it is safe for concurrent use and recognises one
// page at a time.
type Reader struct {
	plat        platform
	languages   func(ctx context.Context) ([]string, error) // replaced in tests
	pageTimeout time.Duration
	slot        chan struct{}
}

// Option configures a Reader.
type Option func(*Reader)

// WithPageTimeout sets the time limit of one page; non-positive values are ignored.
func WithPageTimeout(d time.Duration) Option {
	return func(r *Reader) {
		if d > 0 {
			r.pageTimeout = d
		}
	}
}

// New returns a Reader backed by Windows OCR (ErrUnsupported for every call on other systems).
func New(opts ...Option) *Reader {
	return newReader(newPlatform(), opts...)
}

func newReader(p platform, opts ...Option) *Reader {
	r := &Reader{plat: p, pageTimeout: DefaultPageTimeout, slot: make(chan struct{}, 1)}
	if p != nil {
		r.languages = p.languages
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// AvailableLanguages returns the BCP-47 tags of the OCR recognizers installed in Windows, e.g. ["en-US", "ru"].
func (r *Reader) AvailableLanguages(ctx context.Context) ([]string, error) {
	if r.plat == nil {
		return nil, ErrUnsupported
	}
	pctx, cancel := context.WithTimeout(ctx, r.pageTimeout)
	defer cancel()

	var tags []string
	err := r.run(pctx, func(ctx context.Context) error {
		var err error
		tags, err = r.languages(ctx)
		return err
	})
	if err != nil {
		return nil, r.mapErr(ctx, err)
	}
	return tags, nil
}

// Read recognises the printed text of a PNG or JPEG image with the Russian recognizer. Errors: ErrUnsupported,
// ErrNoLanguage, ErrBadImage, ErrTooLarge, ErrTimeout, ErrEngine (HResultError), or the context's error.
func (r *Reader) Read(ctx context.Context, img []byte) (Page, error) {
	if r.plat == nil {
		return Page{}, ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	hdr, err := checkImage(img)
	if err != nil {
		return Page{}, err
	}
	pctx, cancel := context.WithTimeout(ctx, r.pageTimeout)
	defer cancel()

	var page Page
	err = r.run(pctx, func(ctx context.Context) error {
		tags, err := r.languages(ctx)
		if err != nil {
			return err
		}
		if !hasLanguage(tags, Language) {
			return ErrNoLanguage
		}

		var bw, bh int // size of the bitmap the engine saw
		lines, tag, err := r.plat.recognize(ctx, Language, func(maxDim int) (*image.Gray, error) {
			g, err := decodeGray(img, hdr)
			if err != nil {
				return nil, err
			}
			g = fit(g, maxDim)
			bw, bh = g.Rect.Dx(), g.Rect.Dy()
			return g, nil
		})
		if err != nil {
			return err
		}
		if bw == 0 || bh == 0 {
			return fmt.Errorf("%w: the engine returned without the image", ErrEngine)
		}
		if !hasLanguage([]string{tag}, Language) {
			return fmt.Errorf("%w: the engine recognised with %q", ErrNoLanguage, tag)
		}
		page = Page{
			Lines:  toLines(lines, float64(hdr.width)/float64(bw), float64(hdr.height)/float64(bh), hdr.width, hdr.height),
			Engine: fmt.Sprintf("%s/%s/%s", enginePrefix, tag, r.plat.osVersion()),
		}
		return nil
	})
	if err != nil {
		return Page{}, r.mapErr(ctx, err)
	}
	return page, nil
}

// run calls fn on its own goroutine, one call at a time, and returns when fn ends or ctx is done. A cancelled call
// returns at once; fn sees the same ctx, stops the engine and frees the slot for the next call.
func (r *Reader) run(ctx context.Context, fn func(context.Context) error) error {
	select {
	case r.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		defer func() { <-r.slot }()
		done <- protect(ctx, fn)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		select {
		case err := <-done: // finished at the same moment: keep the real outcome
			return err
		default:
			return ctx.Err()
		}
	}
}

// protect turns a panic of the engine section into ErrEngine.
func protect(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: panic: %v", ErrEngine, p)
		}
	}()
	return fn(ctx)
}

// mapErr gives the caller's context error priority and turns the page deadline into ErrTimeout.
func (r *Reader) mapErr(parent context.Context, err error) error {
	if perr := parent.Err(); perr != nil {
		return perr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: over %s", ErrTimeout, r.pageTimeout)
	}
	return err
}

// hasLanguage reports whether tags hold lang or one of its regional variants ("ru", "ru-RU").
func hasLanguage(tags []string, lang string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, lang) || len(t) > len(lang) && strings.EqualFold(t[:len(lang)+1], lang+"-") {
			return true
		}
	}
	return false
}

// toLines turns the engine's lines into boxes of the input image: the union of the word boxes, scaled back by
// (sx, sy) and clamped to w×h. A line without words gets an empty box.
func toLines(raw []rawLine, sx, sy float64, w, h int) []Line {
	out := make([]Line, 0, len(raw))
	for _, l := range raw {
		line := Line{Text: l.text}
		if len(l.words) > 0 {
			x0, y0 := math.Inf(1), math.Inf(1)
			x1, y1 := math.Inf(-1), math.Inf(-1)
			for _, r := range l.words {
				x0, y0 = min(x0, float64(r.X)), min(y0, float64(r.Y))
				x1, y1 = max(x1, float64(r.X+r.Width)), max(y1, float64(r.Y+r.Height))
			}
			x0, y0, x1, y1 = x0*sx, y0*sy, x1*sx, y1*sy
			if finite(x0, y0, x1, y1) {
				box := image.Rect(int(math.Floor(x0)), int(math.Floor(y0)), int(math.Ceil(x1)), int(math.Ceil(y1)))
				line.Box = box.Intersect(image.Rect(0, 0, w, h))
			}
		}
		out = append(out, line)
	}
	return out
}

// finite guards the float-to-int conversion: the engine's boxes are untrusted numbers.
func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxInt32 {
			return false
		}
	}
	return true
}
