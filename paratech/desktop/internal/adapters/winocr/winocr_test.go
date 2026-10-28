package winocr

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePlatform stands in for Windows OCR in the tests of the shared logic.
type fakePlatform struct {
	langs  []string
	maxDim int
	lines  []rawLine
	tag    string
	block  atomic.Bool // wait for the context instead of recognising
	panics string      // panic with this value
	err    error

	mu      sync.Mutex
	gotSize image.Point
	active  atomic.Int32
	overlap atomic.Bool
}

func (f *fakePlatform) languages(ctx context.Context) ([]string, error) { return f.langs, ctx.Err() }

func (f *fakePlatform) osVersion() string { return "10.0.1" }

func (f *fakePlatform) recognize(ctx context.Context, _ string, prepare func(int) (*image.Gray, error)) ([]rawLine, string, error) {
	if f.active.Add(1) > 1 {
		f.overlap.Store(true)
	}
	defer f.active.Add(-1)
	if f.panics != "" {
		panic(f.panics)
	}
	g, err := prepare(f.maxDim)
	if err != nil {
		return nil, "", err
	}
	f.mu.Lock()
	f.gotSize = g.Rect.Size()
	f.mu.Unlock()
	if f.block.Load() {
		<-ctx.Done()
		return nil, "", ctx.Err()
	}
	if f.err != nil {
		return nil, "", f.err
	}
	time.Sleep(time.Millisecond)
	return f.lines, f.tag, nil
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func grayImage(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = uint8(i % 251) //nolint:gosec // below 256
	}
	return g
}

func russianOnly() *fakePlatform {
	return &fakePlatform{langs: []string{"en-US", "ru"}, maxDim: 10000, tag: "ru",
		lines: []rawLine{{text: "Привет", words: []rectF{{X: 1, Y: 2, Width: 3, Height: 4}}}}}
}

func TestReadWithFakeEngine(t *testing.T) {
	p := russianOnly()
	page, err := newReader(p).Read(context.Background(), encodePNG(t, grayImage(40, 20)))
	if err != nil {
		t.Fatal(err)
	}
	want := Page{Lines: []Line{{Text: "Привет", Box: image.Rect(1, 2, 4, 6)}}, Engine: "windows-ocr/ru/10.0.1"}
	if fmt.Sprint(page) != fmt.Sprint(want) {
		t.Fatalf("page = %+v, want %+v", page, want)
	}
}

func TestReadErrors(t *testing.T) {
	png40 := encodePNG(t, grayImage(40, 20))
	jpg40 := encodeJPEG(t, grayImage(40, 20))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	engineErr := &HResultError{Op: "OcrEngine.RecognizeAsync", Code: 0x80004005}

	tests := []struct {
		name string
		plat func() *fakePlatform
		ctx  context.Context
		img  []byte
		want error
	}{
		{"english only", func() *fakePlatform { p := russianOnly(); p.langs = []string{"en-US"}; return p }, context.Background(), png40, ErrNoLanguage},
		{"no languages", func() *fakePlatform { p := russianOnly(); p.langs = nil; return p }, context.Background(), png40, ErrNoLanguage},
		{"engine of another language", func() *fakePlatform { p := russianOnly(); p.tag = "en-US"; return p }, context.Background(), png40, ErrNoLanguage},
		{"engine without a language", func() *fakePlatform { p := russianOnly(); p.tag = ""; return p }, context.Background(), png40, ErrNoLanguage},
		{"engine of a look-alike tag", func() *fakePlatform { p := russianOnly(); p.tag = "rus"; return p }, context.Background(), png40, ErrNoLanguage},
		{"engine of a regional russian", func() *fakePlatform { p := russianOnly(); p.tag = "ru-RU"; return p }, context.Background(), png40, nil},
		{"truncated png", russianOnly, context.Background(), png40[:len(png40)/2], ErrBadImage},
		{"truncated jpeg", russianOnly, context.Background(), jpg40[:len(jpg40)/2], ErrBadImage},
		{"png header only", russianOnly, context.Background(), png40[:33], ErrBadImage},
		{"empty", russianOnly, context.Background(), nil, ErrBadImage},
		{"gif", russianOnly, context.Background(), []byte("GIF89a\x01\x00\x01\x00"), ErrBadImage},
		{"over the byte limit", russianOnly, context.Background(), append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 25<<20)...), ErrTooLarge},
		{"over the pixel limit", russianOnly, context.Background(), pngHeader(t, 8000, 6000), ErrTooLarge},
		{"cancelled context", russianOnly, cancelled, png40, context.Canceled},
		{"engine error", func() *fakePlatform { p := russianOnly(); p.err = engineErr; return p }, context.Background(), png40, ErrEngine},
		{"engine panic", func() *fakePlatform { p := russianOnly(); p.panics = "boom"; return p }, context.Background(), png40, ErrEngine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newReader(tt.plat()).Read(tt.ctx, tt.img)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Read = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestErrNoLanguageTellsHowToInstall: the sentinel is what the user sees until the UI maps it.
func TestErrNoLanguageTellsHowToInstall(t *testing.T) {
	msg := ErrNoLanguage.Error()
	for _, want := range []string{"Time & language", "Russian", "Optical character recognition", "restart"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrNoLanguage = %q, want it to mention %q", msg, want)
		}
	}
}

// pngHeader returns the signature and IHDR chunk of an 8-bit grey PNG of w×h: enough for DecodeConfig, nothing to
// decode, like a decompression bomb before its data.
func pngHeader(t *testing.T, w, h uint32) []byte {
	t.Helper()
	chunk := binary.BigEndian.AppendUint32(nil, 13)
	chunk = append(chunk, "IHDR"...)
	chunk = binary.BigEndian.AppendUint32(chunk, w)
	chunk = binary.BigEndian.AppendUint32(chunk, h)
	chunk = append(chunk, 8, 0, 0, 0, 0) // depth, grey, deflate, adaptive filter, not interlaced
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	return append([]byte("\x89PNG\r\n\x1a\n"), chunk...)
}

func TestReadCancelWhileRecognising(t *testing.T) {
	p := russianOnly()
	p.block.Store(true)
	r := newReader(p)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	start := time.Now()
	if _, err := r.Read(ctx, encodePNG(t, grayImage(10, 10))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancel took %v", d)
	}

	p.block.Store(false) // the slot is free again once the cancelled call ended
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := r.Read(ctx2, encodePNG(t, grayImage(10, 10))); err != nil {
		t.Fatalf("Read after cancel: %v", err)
	}
}

func TestReadTimeout(t *testing.T) {
	p := russianOnly()
	p.block.Store(true)
	_, err := newReader(p, WithPageTimeout(20*time.Millisecond)).Read(context.Background(), encodePNG(t, grayImage(10, 10)))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Read = %v, want ErrTimeout", err)
	}
}

func TestReadSerialises(t *testing.T) {
	p := russianOnly()
	r := newReader(p)
	img := encodePNG(t, grayImage(30, 30))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := r.Read(context.Background(), img); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if p.overlap.Load() {
		t.Fatal("two recognitions ran at once")
	}
}

func TestReadDownscalesToMaxDimension(t *testing.T) {
	p := russianOnly()
	p.maxDim = 100
	p.lines = []rawLine{{text: "x", words: []rectF{{X: 10, Y: 5, Width: 20, Height: 10}}}}
	page, err := newReader(p).Read(context.Background(), encodePNG(t, grayImage(400, 200)))
	if err != nil {
		t.Fatal(err)
	}
	if p.gotSize != (image.Point{X: 100, Y: 50}) {
		t.Fatalf("engine saw %v, want 100x50", p.gotSize)
	}
	if want := image.Rect(40, 20, 120, 60); page.Lines[0].Box != want {
		t.Fatalf("box = %v, want %v (scaled back by 4)", page.Lines[0].Box, want)
	}
}

func TestAvailableLanguages(t *testing.T) {
	p := russianOnly()
	got, err := newReader(p).AvailableLanguages(context.Background())
	if err != nil || fmt.Sprint(got) != "[en-US ru]" {
		t.Fatalf("AvailableLanguages = %v, %v", got, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newReader(p).AvailableLanguages(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestNilPlatformIsUnsupported(t *testing.T) {
	r := newReader(nil)
	if _, err := r.Read(context.Background(), encodePNG(t, grayImage(4, 4))); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Read = %v", err)
	}
	if _, err := r.AvailableLanguages(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("AvailableLanguages = %v", err)
	}
}

func TestWithPageTimeout(t *testing.T) {
	if got := newReader(nil).pageTimeout; got != DefaultPageTimeout {
		t.Fatalf("default = %v", got)
	}
	if got := newReader(nil, WithPageTimeout(0), WithPageTimeout(-1)).pageTimeout; got != DefaultPageTimeout {
		t.Fatalf("non-positive must be ignored, got %v", got)
	}
	if got := newReader(nil, WithPageTimeout(time.Second)).pageTimeout; got != time.Second {
		t.Fatalf("got %v", got)
	}
}

func TestHResultError(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &HResultError{Op: "RoInitialize", Code: 0x80010106})
	if !errors.Is(err, ErrEngine) {
		t.Fatal("HResultError must match ErrEngine")
	}
	var h *HResultError
	if !errors.As(err, &h) || h.Code != 0x80010106 {
		t.Fatalf("errors.As = %v", h)
	}
	if want := "winocr: RoInitialize failed with HRESULT 0x80010106"; h.Error() != want {
		t.Fatalf("Error() = %q", h.Error())
	}
}

func TestHasLanguage(t *testing.T) {
	tests := []struct {
		tags []string
		want bool
	}{
		{[]string{"ru"}, true},
		{[]string{"en-US", "ru-RU"}, true},
		{[]string{"RU"}, true},
		{[]string{"en-US"}, false},
		{[]string{"rue"}, false},
		{[]string{"r"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := hasLanguage(tt.tags, "ru"); got != tt.want {
			t.Errorf("hasLanguage(%v) = %v, want %v", tt.tags, got, tt.want)
		}
	}
}

func TestToLines(t *testing.T) {
	nan := float32(math.NaN())
	tests := []struct {
		name   string
		raw    rawLine
		sx, sy float64
		want   image.Rectangle
	}{
		{"union of words", rawLine{words: []rectF{{X: 10, Y: 10, Width: 5, Height: 5}, {X: 20, Y: 8, Width: 5, Height: 4}}}, 1, 1, image.Rect(10, 8, 25, 15)},
		{"scaled and rounded outwards", rawLine{words: []rectF{{X: 1.5, Y: 1.5, Width: 1, Height: 1}}}, 2, 3, image.Rect(3, 4, 5, 8)},
		{"clamped to the image", rawLine{words: []rectF{{X: -5, Y: 90, Width: 20, Height: 50}}}, 1, 1, image.Rect(0, 90, 15, 100)},
		{"no words", rawLine{}, 1, 1, image.Rectangle{}},
		{"not finite", rawLine{words: []rectF{{X: nan, Y: 0, Width: 1, Height: 1}}}, 1, 1, image.Rectangle{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toLines([]rawLine{tt.raw}, tt.sx, tt.sy, 100, 100)
			if len(got) != 1 || got[0].Box != tt.want {
				t.Fatalf("toLines = %+v, want box %v", got, tt.want)
			}
		})
	}
}

func TestToGray(t *testing.T) {
	nrgba := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	nrgba.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})   // transparent: white paper
	nrgba.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 255}) // black ink
	rgba := image.NewRGBA(image.Rect(0, 0, 2, 1))
	rgba.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	rgba.SetRGBA(1, 0, color.RGBA{A: 0})
	ycc := image.NewYCbCr(image.Rect(0, 0, 2, 1), image.YCbCrSubsampleRatio444)
	ycc.Y[0], ycc.Y[1] = 10, 200
	pal := image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{color.Black, color.White})
	pal.SetColorIndex(0, 0, 1)
	sub := grayImage(4, 4).SubImage(image.Rect(1, 1, 3, 2)).(*image.Gray)

	tests := []struct {
		name string
		img  image.Image
		want []uint8
	}{
		{"nrgba over white", nrgba, []uint8{255, 0}},
		{"rgba over white", rgba, []uint8{255, 255}},
		{"ycbcr luma", ycc, []uint8{10, 200}},
		{"paletted (generic path)", pal, []uint8{255, 0}},
		{"gray sub-image", sub, []uint8{5, 6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := toGray(tt.img)
			if g.Rect.Min != (image.Point{}) || !bytes.Equal(g.Pix, tt.want) {
				t.Fatalf("toGray = %v %v, want %v", g.Rect, g.Pix, tt.want)
			}
		})
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		maxDim int
		want   image.Point
	}{
		{"fits", 100, 50, 100, image.Pt(100, 50)},
		{"no limit", 100, 50, 0, image.Pt(100, 50)},
		{"wide", 3000, 1000, 1000, image.Pt(1000, 333)},
		{"tall", 1000, 12000, 10000, image.Pt(833, 10000)},
		{"thin line", 5000, 1, 1000, image.Pt(1000, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fit(grayImage(tt.w, tt.h), tt.maxDim).Rect.Size(); got != tt.want {
				t.Fatalf("fit = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResizeAverages(t *testing.T) {
	g := image.NewGray(image.Rect(0, 0, 4, 2)) // checkerboard of 0 and 255
	for i := range g.Pix {
		if (i%4+i/4)%2 == 0 {
			g.Pix[i] = 255
		}
	}
	got := resize(g, 2, 1)
	if !bytes.Equal(got.Pix, []uint8{128, 128}) {
		t.Fatalf("resize = %v, want two mid greys", got.Pix)
	}

	flat := image.NewGray(image.Rect(0, 0, 7, 5))
	for i := range flat.Pix {
		flat.Pix[i] = 77
	}
	for _, p := range resize(flat, 3, 2).Pix {
		if p != 77 {
			t.Fatalf("a flat image must stay flat, got %d", p)
		}
	}
}

func TestSpanCoversSource(t *testing.T) {
	for _, c := range []struct{ n, m int }{{10, 3}, {7, 7}, {12000, 10000}, {5, 1}} {
		next := 0
		for i := range c.m {
			lo, hi := span(i, c.n, c.m)
			if lo != next || hi <= lo {
				t.Fatalf("span(%d, %d, %d) = [%d, %d), want start %d", i, c.n, c.m, lo, hi, next)
			}
			next = hi
		}
		if next != c.n {
			t.Fatalf("n=%d m=%d: spans end at %d", c.n, c.m, next)
		}
	}
}
