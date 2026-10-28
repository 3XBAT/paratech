//go:build windows

package winocr

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // G505: the WinRT pinterface IID algorithm is UUIDv5, defined over SHA-1
	"errors"
	"image"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// envWinOCRCI=1 runs the real-OCR tests on GitHub Actions too. They are off there by default, like the real-WebView2
// tests of adapters/pdf: the hosted runner has no Russian recognizer and may lack the OCR classes. Locally they run.
const envWinOCRCI = "LA_WINOCR_CI_TEST"

func realOCROff(getenv func(string) string) bool {
	return getenv("GITHUB_ACTIONS") == "true" && getenv(envWinOCRCI) != "1"
}

// requireRussianOCR skips tests that need the real engine with the Russian recognizer.
func requireRussianOCR(t *testing.T) {
	t.Helper()
	if realOCROff(os.Getenv) {
		t.Skipf("real Windows OCR tests are off on GitHub Actions; set %s=1 to run them", envWinOCRCI)
	}
	langs, err := New().AvailableLanguages(context.Background())
	if errors.Is(err, ErrUnsupported) {
		t.Skipf("Windows OCR is not available here: %v", err)
	}
	if err != nil {
		t.Fatalf("AvailableLanguages: %v", err)
	}
	if !hasLanguage(langs, Language) {
		t.Skipf("no Russian OCR recognizer installed (have %v): Settings → Time & Language → Language → Russian → Optical character recognition", langs)
	}
}

// fixtureKeywords are words of testdata/printed-ru.png; OCR noise may lose a few.
var fixtureKeywords = []string{"лекция", "математическому", "анализу", "предел", "функции", "непрерывность",
	"домашнее", "задание", "решить", "задачи", "пятнице"}

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/printed-ru.png")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// checkKeywords requires at least 80% of the keywords in the recognised text (lower-cased, ё as е).
func checkKeywords(t *testing.T, page Page) {
	t.Helper()
	var all []string
	for _, l := range page.Lines {
		all = append(all, l.Text)
	}
	text := strings.ReplaceAll(strings.ToLower(strings.Join(all, "\n")), "ё", "е")
	found := 0
	for _, k := range fixtureKeywords {
		if strings.Contains(text, k) {
			found++
		} else {
			t.Logf("keyword %q not recognised", k)
		}
	}
	if found*5 < len(fixtureKeywords)*4 {
		t.Fatalf("recognised %d of %d keywords; text:\n%s", found, len(fixtureKeywords), text)
	}
}

func TestRealOCROff(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"local", nil, false},
		{"github actions", map[string]string{"GITHUB_ACTIONS": "true"}, true},
		{"github actions opted in", map[string]string{"GITHUB_ACTIONS": "true", envWinOCRCI: "1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := realOCROff(env(tt.env)); got != tt.want {
				t.Fatalf("realOCROff = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMustGUID(t *testing.T) {
	got := mustGUID("00000036-0000-0000-c000-000000000046")
	want := windows.GUID{Data1: 0x36, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	if got != want {
		t.Fatalf("mustGUID = %+v, want %+v", got, want)
	}
	for _, bad := range []string{"", "00000036-0000-0000-c000", "zz000036-0000-0000-c000-000000000046"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("mustGUID(%q) did not panic", bad)
				}
			}()
			mustGUID(bad)
		}()
	}
}

// TestEnglishOnlyIsNoLanguage needs no OCR: the injected lookup says only English is installed.
func TestEnglishOnlyIsNoLanguage(t *testing.T) {
	r := New()
	r.languages = func(context.Context) ([]string, error) { return []string{"en-US"}, nil }
	if _, err := r.Read(context.Background(), readFixture(t)); !errors.Is(err, ErrNoLanguage) {
		t.Fatalf("Read = %v, want ErrNoLanguage", err)
	}
}

func TestAvailableLanguagesReal(t *testing.T) {
	if realOCROff(os.Getenv) {
		t.Skipf("real Windows OCR tests are off on GitHub Actions; set %s=1 to run them", envWinOCRCI)
	}
	langs, err := New().AvailableLanguages(context.Background())
	if errors.Is(err, ErrUnsupported) {
		t.Skipf("Windows OCR is not available here: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("installed OCR recognizers: %v", langs)
}

func TestReadFixtureReal(t *testing.T) {
	requireRussianOCR(t)
	page, err := New().Read(context.Background(), readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("engine %s, %d lines", page.Engine, len(page.Lines))
	for _, l := range page.Lines {
		t.Logf("%v %q", l.Box, l.Text)
	}
	if !strings.HasPrefix(page.Engine, enginePrefix+"/ru") || strings.Count(page.Engine, "/") != 2 {
		t.Fatalf("engine = %q", page.Engine)
	}
	checkKeywords(t, page)
	bounds := image.Rect(0, 0, 1400, 420)
	for _, l := range page.Lines {
		if l.Box.Empty() || !l.Box.In(bounds) {
			t.Fatalf("line %q box %v outside %v", l.Text, l.Box, bounds)
		}
	}
}

// TestReadDownscalesReal puts the fixture on a canvas taller than MaxImageDimension: the page is downscaled for the
// engine and the boxes come back in pixels of the canvas.
func TestReadDownscalesReal(t *testing.T) {
	requireRussianOCR(t)
	page, err := New().Read(context.Background(), tallCanvas(t))
	if err != nil {
		t.Fatal(err)
	}
	checkKeywords(t, page)
	for _, l := range page.Lines {
		if l.Box.Max.Y > 440 || l.Box.Min.Y < 0 {
			t.Fatalf("line %q box %v not where the text is (rows 0..420 of the canvas)", l.Text, l.Box)
		}
	}
}

// tallCanvas is a PNG with the fixture at the top of a white canvas 20% taller than MaxImageDimension, so Read
// downscales it.
func tallCanvas(t *testing.T) []byte {
	t.Helper()
	maxDim := realMaxDimension(t)
	src, err := png.Decode(bytes.NewReader(readFixture(t)))
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewGray(image.Rect(0, 0, 900, maxDim+maxDim/5)) // the text ends at x = 840
	draw.Draw(canvas, canvas.Rect, image.White, image.Point{}, draw.Src)
	draw.Draw(canvas, src.Bounds(), src, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, canvas); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestAwaitCancelsReal hands await a cancelled context while the engine runs: it calls IAsyncInfo.Cancel and returns
// the context's error without waiting for the result.
func TestAwaitCancelsReal(t *testing.T) {
	requireRussianOCR(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := session(func() error {
		statics, err := activationFactory(classOcrEngine, &iidIOcrEngineStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		engine, _, err := newEngine(statics, Language)
		if err != nil {
			return err
		}
		defer engine.release()
		bmp, err := newGrayBitmap(grayImage(4000, 4000))
		if err != nil {
			return err
		}
		defer bmp.release()
		op, err := engine.getObjectArg("RecognizeAsync", slotRecognizeAsync, bmp)
		if err != nil {
			return err
		}
		defer op.release()
		res, err := await(ctx, op)
		res.release()
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("await = %v, want context.Canceled", err)
	}
}

// TestNewEngineIsRussianReal: the engine Windows creates for "ru" reports a Russian recognizer tag.
func TestNewEngineIsRussianReal(t *testing.T) {
	requireRussianOCR(t)
	var tag string
	err := session(func() error {
		statics, err := activationFactory(classOcrEngine, &iidIOcrEngineStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		engine, got, err := newEngine(statics, Language)
		if err != nil {
			return err
		}
		engine.release()
		tag = got
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLanguage([]string{tag}, Language) {
		t.Fatalf("recognizer tag = %q, want %q", tag, Language)
	}
}

// TestRecognizeStopsAfterPrepareReal cancels the context inside prepare and hands back an empty image: recognize
// returns the context's error before building the bitmap (which would fail with ErrBadImage).
func TestRecognizeStopsAfterPrepareReal(t *testing.T) {
	requireRussianOCR(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, err := winPlatform{}.recognize(ctx, Language, func(int) (*image.Gray, error) {
		cancel()
		return image.NewGray(image.Rect(0, 0, 0, 0)), nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("recognize = %v, want context.Canceled", err)
	}
}

func realMaxDimension(t *testing.T) int {
	t.Helper()
	var n uint32
	err := session(func() error {
		statics, err := activationFactory(classOcrEngine, &iidIOcrEngineStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		n, err = statics.getUint32("MaxImageDimension", slotMaxImageDimension)
		return err
	})
	if err != nil || n == 0 {
		t.Fatalf("MaxImageDimension = %d, %v", n, err)
	}
	t.Logf("OcrEngine.MaxImageDimension = %d", n)
	return int(n)
}

// TestReadCancelReal cancels a running recognition of a large page: Read returns the context's error at once and the
// next Read works (the cancelled one stopped the engine and freed the slot).
func TestReadCancelReal(t *testing.T) {
	requireRussianOCR(t)
	r := New()
	tall := tallCanvas(t)
	ctx, cancel := context.WithCancel(context.Background())
	var cancelledAt time.Time
	timer := time.AfterFunc(50*time.Millisecond, func() {
		cancelledAt = time.Now()
		cancel()
	})
	defer timer.Stop()
	if _, err := r.Read(ctx, tall); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read = %v, want context.Canceled", err)
	}
	if d := time.Since(cancelledAt); d > time.Second {
		t.Fatalf("Read returned %v after the cancel", d)
	}

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := r.Read(cancelled, readFixture(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read with a cancelled context = %v", err)
	}

	page, err := r.Read(context.Background(), readFixture(t))
	if err != nil {
		t.Fatalf("Read after a cancel: %v", err)
	}
	checkKeywords(t, page)
}

func TestReadTimeoutReal(t *testing.T) {
	requireRussianOCR(t)
	_, err := New(WithPageTimeout(time.Millisecond)).Read(context.Background(), readFixture(t))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Read = %v, want ErrTimeout", err)
	}
}

func TestReadConcurrentReal(t *testing.T) {
	requireRussianOCR(t)
	r := New()
	img := readFixture(t)
	errs := make(chan error, 3)
	for range 3 {
		go func() {
			page, err := r.Read(context.Background(), img)
			if err == nil && len(page.Lines) == 0 {
				err = errors.New("no lines")
			}
			errs <- err
		}()
	}
	for range 3 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// pinterfaceIID is the IID of a parameterized interface instance: UUIDv5 (SHA-1) in the WinRT namespace over the
// UTF-8 type signature (Windows Runtime type system, «Guid generation for parameterized types»).
func pinterfaceIID(signature string) windows.GUID {
	ns := []byte{0x11, 0xf4, 0x7a, 0xd5, 0x7b, 0x73, 0x42, 0xc0, 0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee}
	sum := sha1.Sum(append(ns, signature...)) //nolint:gosec // G401: UUIDv5 is defined over SHA-1
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	return windows.GUID{
		Data1: uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]),
		Data2: uint16(b[4])<<8 | uint16(b[5]),
		Data3: uint16(b[6])<<8 | uint16(b[7]),
		Data4: [8]byte(b[8:16]),
	}
}

// TestPinterfaceIID compares with the values in docs/refs/windows-ocr.md, computed independently by Python's
// uuid.uuid5; TestObjectsImplementTheInterfacesReal then checks them against Windows itself.
func TestPinterfaceIID(t *testing.T) {
	tests := []struct{ sig, want string }{
		{"pinterface({9fc2b0bb-e446-44e2-aa61-9cab8f636af2};rc(Windows.Media.Ocr.OcrResult;{9bd235b2-175b-3d6a-92e2-388c206e2f63}))", "c7d7118e-ae36-59c0-ac76-7badee711c8b"},
		{"pinterface({bbe1fa4c-b0e3-4583-baef-1f1b2e483e56};rc(Windows.Globalization.Language;{ea79a752-f7c2-4265-b1bd-c4dec4e4f080}))", "144b0f3d-2d59-5dd2-b012-908ec3e06435"},
		{"pinterface({bbe1fa4c-b0e3-4583-baef-1f1b2e483e56};rc(Windows.Media.Ocr.OcrLine;{0043a16f-e31f-3a24-899c-d444bd088124}))", "60c76eac-8875-5ddb-a19b-65a3936279ea"},
		{"pinterface({bbe1fa4c-b0e3-4583-baef-1f1b2e483e56};rc(Windows.Media.Ocr.OcrWord;{3c2a477a-5cd9-3525-ba2a-23d1e0a68a1d}))", "805a60c7-df4f-527c-86b2-e29e439a83d2"},
	}
	for _, tt := range tests {
		if got, want := pinterfaceIID(tt.sig), mustGUID(tt.want); got != want {
			t.Errorf("%s = %v, want %v", tt.sig, got, want)
		}
	}
}

// TestObjectsImplementTheInterfacesReal proves the slots are called on the right interfaces: every object received
// from an out-parameter answers QueryInterface for the parameterized IID computed from the metadata.
func TestObjectsImplementTheInterfacesReal(t *testing.T) {
	requireRussianOCR(t)
	const (
		asyncOp    = "{9fc2b0bb-e446-44e2-aa61-9cab8f636af2}"
		vectorView = "{bbe1fa4c-b0e3-4583-baef-1f1b2e483e56}"
	)
	iids := map[string]windows.GUID{
		"IAsyncOperation<OcrResult>": pinterfaceIID("pinterface(" + asyncOp + ";rc(Windows.Media.Ocr.OcrResult;{9bd235b2-175b-3d6a-92e2-388c206e2f63}))"),
		"IVectorView<Language>":      pinterfaceIID("pinterface(" + vectorView + ";rc(Windows.Globalization.Language;{ea79a752-f7c2-4265-b1bd-c4dec4e4f080}))"),
		"IVectorView<OcrLine>":       pinterfaceIID("pinterface(" + vectorView + ";rc(Windows.Media.Ocr.OcrLine;{0043a16f-e31f-3a24-899c-d444bd088124}))"),
		"IVectorView<OcrWord>":       pinterfaceIID("pinterface(" + vectorView + ";rc(Windows.Media.Ocr.OcrWord;{3c2a477a-5cd9-3525-ba2a-23d1e0a68a1d}))"),
	}
	expect := func(o *comObject, name string) {
		t.Helper()
		iid := iids[name]
		q, err := o.queryInterface("QueryInterface "+name, &iid)
		if err != nil {
			t.Fatalf("%s %v: %v", name, iid, err)
		}
		q.release()
	}
	plain := map[string]windows.GUID{
		"IOcrEngine": mustGUID("5a14bc41-5b76-3140-b680-8825562683ac"),
		"IOcrResult": mustGUID("9bd235b2-175b-3d6a-92e2-388c206e2f63"),
		"IOcrLine":   mustGUID("0043a16f-e31f-3a24-899c-d444bd088124"),
		"IOcrWord":   mustGUID("3c2a477a-5cd9-3525-ba2a-23d1e0a68a1d"),
		"ILanguage":  mustGUID("ea79a752-f7c2-4265-b1bd-c4dec4e4f080"),
	}
	expectPlain := func(o *comObject, name string) {
		t.Helper()
		iid := plain[name]
		q, err := o.queryInterface("QueryInterface "+name, &iid)
		if err != nil {
			t.Fatalf("%s %v: %v", name, iid, err)
		}
		q.release()
	}

	err := session(func() error {
		statics, err := activationFactory(classOcrEngine, &iidIOcrEngineStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		langs, err := statics.getObject("AvailableRecognizerLanguages", slotAvailableRecognizerLanguages)
		if err != nil {
			return err
		}
		defer langs.release()
		expect(langs, "IVectorView<Language>")

		engine, _, err := newEngine(statics, Language)
		if err != nil {
			return err
		}
		defer engine.release()
		expectPlain(engine, "IOcrEngine")
		lang, err := engine.getObject("RecognizerLanguage", slotRecognizerLanguage)
		if err != nil {
			return err
		}
		defer lang.release()
		expectPlain(lang, "ILanguage")

		hdr, err := checkImage(readFixture(t))
		if err != nil {
			return err
		}
		g, err := decodeGray(readFixture(t), hdr)
		if err != nil {
			return err
		}
		bmp, err := newGrayBitmap(g)
		if err != nil {
			return err
		}
		defer bmp.release()
		op, err := engine.getObjectArg("RecognizeAsync", slotRecognizeAsync, bmp)
		if err != nil {
			return err
		}
		defer op.release()
		expect(op, "IAsyncOperation<OcrResult>")

		result, err := await(context.Background(), op)
		if err != nil {
			return err
		}
		defer result.release()
		expectPlain(result, "IOcrResult")
		lines, err := result.getObject("Lines", slotResultLines)
		if err != nil {
			return err
		}
		defer lines.release()
		expect(lines, "IVectorView<OcrLine>")
		line, err := lines.getObjectWord("GetAt", slotGetAt, 0)
		if err != nil {
			return err
		}
		defer line.release()
		expectPlain(line, "IOcrLine")
		words, err := line.getObject("Words", slotLineWords)
		if err != nil {
			return err
		}
		defer words.release()
		expect(words, "IVectorView<OcrWord>")
		word, err := words.getObjectWord("GetAt", slotGetAt, 0)
		if err != nil {
			return err
		}
		defer word.release()
		expectPlain(word, "IOcrWord")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHStringRoundTrip(t *testing.T) {
	if err := findProcs(); err != nil {
		t.Skip(err)
	}
	for _, s := range []string{"", "ru", "Распознавание текста"} {
		h, err := newHString(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := takeString(h); got != s {
			t.Fatalf("round trip %q = %q", s, got)
		}
	}
	if _, err := newHString("a\x00b"); !errors.Is(err, ErrEngine) {
		t.Fatalf("NUL string: %v", err)
	}
}
