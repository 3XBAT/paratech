//go:build windows

package winocr

import (
	"context"
	"encoding/hex"
	"fmt"
	"image"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// Runtime classes (Windows metadata, docs/refs/windows-ocr.md).
const (
	classOcrEngine           = "Windows.Media.Ocr.OcrEngine"
	classLanguage            = "Windows.Globalization.Language"
	classSoftwareBitmap      = "Windows.Graphics.Imaging.SoftwareBitmap"
	classCryptographicBuffer = "Windows.Security.Cryptography.CryptographicBuffer"
)

// IIDs from the GuidAttribute of each interface in C:\Windows\System32\WinMetadata\*.winmd.
var (
	iidIOcrEngineStatics           = mustGUID("5bffa85a-3384-3540-9940-699120d428a8")
	iidILanguageFactory            = mustGUID("9b0252ac-0c27-44f8-b792-9793fb66c63e")
	iidISoftwareBitmapStatics      = mustGUID("df0385db-672f-4a9d-806e-c2442f343e86")
	iidICryptographicBufferStatics = mustGUID("320b7e22-3cb0-4cdf-8663-1d28910065eb")
	iidIAsyncInfo                  = mustGUID("00000036-0000-0000-c000-000000000046")
)

// Vtable slots: 6 (IUnknown + IInspectable) + the method's index in its interface in the metadata.
const (
	// IOcrEngineStatics
	slotMaxImageDimension            = 6
	slotAvailableRecognizerLanguages = 7
	slotTryCreateFromLanguage        = 9

	// IOcrEngine
	slotRecognizeAsync      = 6
	slotRecognizerLanguage  = 7
	slotResultLines         = 6 // IOcrResult.get_Lines
	slotLineWords           = 6 // IOcrLine.get_Words
	slotLineText            = 7 // IOcrLine.get_Text
	slotWordBoundingRect    = 6 // IOcrWord.get_BoundingRect
	slotWordText            = 7 // IOcrWord.get_Text
	slotCreateLanguage      = 6 // ILanguageFactory.CreateLanguage
	slotLanguageTag         = 6 // ILanguage.get_LanguageTag
	slotCreateFromByteArray = 9 // ICryptographicBufferStatics.CreateFromByteArray
	slotCreateCopyFromBuf   = 9 // ISoftwareBitmapStatics.CreateCopyFromBuffer (no alpha)

	// IVectorView<T>
	slotGetAt = 6
	slotSize  = 7

	// IAsyncInfo
	slotAsyncStatus    = 7
	slotAsyncErrorCode = 8
	slotAsyncCancel    = 9
	slotAsyncClose     = 10

	// IAsyncOperation<T>
	slotGetResults = 8
)

// Enum values from the metadata.
const (
	pixelFormatGray8 = 62 // Windows.Graphics.Imaging.BitmapPixelFormat.Gray8

	asyncCompleted = 1 // Windows.Foundation.AsyncStatus
	asyncCanceled  = 2
	asyncError     = 3
)

// pollInterval is how often a running recognition is checked. Polling IAsyncInfo.Status avoids implementing a
// Completed handler as a COM object; the objects are agile, so the MTA thread needs no message loop.
const pollInterval = 5 * time.Millisecond

// mustGUID parses an IID literal of this file; a malformed literal is a programming error.
func mustGUID(s string) windows.GUID {
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		panic("winocr: bad GUID literal " + s)
	}
	b, err := hex.DecodeString(strings.Join(parts, ""))
	if err != nil || len(b) != 16 {
		panic("winocr: bad GUID literal " + s)
	}
	return windows.GUID{
		Data1: uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]),
		Data2: uint16(b[4])<<8 | uint16(b[5]),
		Data3: uint16(b[6])<<8 | uint16(b[7]),
		Data4: [8]byte(b[8:16]),
	}
}

type winPlatform struct{}

func newPlatform() platform { return winPlatform{} }

// session runs fn with WinRT initialised for the multithreaded apartment on a locked OS thread. RoUninitialize
// matches every successful RoInitialize (S_FALSE included), so the thread goes back to the scheduler clean.
func session(fn func() error) error {
	if err := findProcs(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr := roInitialize()
	if failed(hr) {
		return &HResultError{Op: "RoInitialize", Code: hresult(hr)}
	}
	defer roUninitialize()
	return fn()
}

func (winPlatform) osVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

func (winPlatform) languages(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var tags []string
	err := session(func() error {
		statics, err := activationFactory(classOcrEngine, &iidIOcrEngineStatics)
		if err != nil {
			return err
		}
		defer statics.release()

		view, err := statics.getObject("OcrEngine.AvailableRecognizerLanguages", slotAvailableRecognizerLanguages)
		if err != nil || view == nil {
			return err
		}
		defer view.release()
		return forEach(view, "IVectorView<Language>", func(lang *comObject) error {
			tag, err := lang.getString("Language.LanguageTag", slotLanguageTag)
			if err == nil {
				tags = append(tags, tag)
			}
			return err
		})
	})
	return tags, err
}

func (winPlatform) recognize(ctx context.Context, lang string, prepare func(maxDim int) (*image.Gray, error)) ([]rawLine, string, error) {
	var (
		lines []rawLine
		tag   string
	)
	err := session(func() error {
		statics, err := activationFactory(classOcrEngine, &iidIOcrEngineStatics)
		if err != nil {
			return err
		}
		defer statics.release()

		engine, err := newEngine(statics, lang)
		if err != nil {
			return err
		}
		defer engine.release()

		maxDim, err := statics.getUint32("OcrEngine.MaxImageDimension", slotMaxImageDimension)
		if err != nil {
			return err
		}
		g, err := prepare(int(maxDim))
		if err != nil {
			return err
		}
		bitmap, err := newGrayBitmap(g)
		if err != nil {
			return err
		}
		defer bitmap.release()

		op, err := engine.getObjectArg("OcrEngine.RecognizeAsync", slotRecognizeAsync, bitmap)
		if err != nil {
			return err
		}
		if op == nil {
			return &HResultError{Op: "OcrEngine.RecognizeAsync", Code: uint32(windows.E_POINTER)}
		}
		defer op.release()

		result, err := await(ctx, op)
		if err != nil {
			return err
		}
		defer result.release()

		if lines, err = readLines(result); err != nil {
			return err
		}
		tag, err = recognizerTag(engine)
		return err
	})
	return lines, tag, err
}

// newEngine creates the engine for lang only; a null engine means Windows has no recognizer for it.
func newEngine(statics *comObject, lang string) (*comObject, error) {
	language, err := newLanguage(lang)
	if err != nil {
		return nil, err
	}
	defer language.release()

	engine, err := statics.getObjectArg("OcrEngine.TryCreateFromLanguage", slotTryCreateFromLanguage, language)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		return nil, ErrNoLanguage
	}
	return engine, nil
}

func newLanguage(tag string) (*comObject, error) {
	factory, err := activationFactory(classLanguage, &iidILanguageFactory)
	if err != nil {
		return nil, err
	}
	defer factory.release()

	lang, err := factory.getObjectWithHString("Language.CreateLanguage", slotCreateLanguage, tag)
	if err != nil {
		return nil, err
	}
	if lang == nil {
		return nil, &HResultError{Op: "Language.CreateLanguage", Code: uint32(windows.E_POINTER)}
	}
	return lang, nil
}

// newGrayBitmap copies g into a Gray8 SoftwareBitmap through an IBuffer made by CryptographicBuffer, which avoids
// the classic-COM IBufferByteAccess and any stream or decoder object.
func newGrayBitmap(g *image.Gray) (*comObject, error) {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	pix := g.Pix
	if g.Stride != w || g.Rect.Min != (image.Point{}) || len(pix) != w*h {
		pix = make([]byte, w*h)
		for y := range h {
			copy(pix[y*w:(y+1)*w], g.Pix[g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y):])
		}
	}

	cb, err := activationFactory(classCryptographicBuffer, &iidICryptographicBufferStatics)
	if err != nil {
		return nil, err
	}
	defer cb.release()
	buf, err := cb.createFromBytes("CryptographicBuffer.CreateFromByteArray", slotCreateFromByteArray, pix)
	if err != nil {
		return nil, err
	}
	defer buf.release()

	sbs, err := activationFactory(classSoftwareBitmap, &iidISoftwareBitmapStatics)
	if err != nil {
		return nil, err
	}
	defer sbs.release()
	bmp, err := sbs.createCopyFromBuffer("SoftwareBitmap.CreateCopyFromBuffer", slotCreateCopyFromBuf, buf, pixelFormatGray8, w, h)
	if err != nil {
		return nil, err
	}
	return bmp, nil
}

// await polls the operation until it ends; on ctx done it cancels the operation and returns ctx's error. The
// caller owns the returned result.
func await(ctx context.Context, op *comObject) (*comObject, error) {
	info, err := op.queryInterface("QueryInterface IAsyncInfo", &iidIAsyncInfo)
	if err != nil {
		return nil, err
	}
	defer info.release()

	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		status, err := info.getInt32("IAsyncInfo.Status", slotAsyncStatus)
		if err != nil {
			return nil, err
		}
		switch status {
		case asyncCompleted:
			res, err := op.getObject("IAsyncOperation.GetResults", slotGetResults)
			_ = info.call("IAsyncInfo.Close", slotAsyncClose)
			if err == nil && res == nil {
				err = &HResultError{Op: "IAsyncOperation.GetResults", Code: uint32(windows.E_POINTER)}
			}
			return res, err
		case asyncError:
			code, err := info.getInt32("IAsyncInfo.ErrorCode", slotAsyncErrorCode)
			_ = info.call("IAsyncInfo.Close", slotAsyncClose)
			if err != nil {
				return nil, err
			}
			return nil, &HResultError{Op: "OcrEngine.RecognizeAsync", Code: uint32(code)} //nolint:gosec // HRESULT bits
		case asyncCanceled:
			_ = info.call("IAsyncInfo.Close", slotAsyncClose)
			return nil, fmt.Errorf("%w: recognition cancelled by Windows", ErrEngine)
		}

		select {
		case <-ctx.Done():
			_ = info.call("IAsyncInfo.Cancel", slotAsyncCancel)
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

func readLines(result *comObject) ([]rawLine, error) {
	view, err := result.getObject("OcrResult.Lines", slotResultLines)
	if err != nil || view == nil {
		return nil, err
	}
	defer view.release()

	var lines []rawLine
	err = forEach(view, "IVectorView<OcrLine>", func(line *comObject) error {
		text, err := line.getString("OcrLine.Text", slotLineText)
		if err != nil {
			return err
		}
		words, err := line.getObject("OcrLine.Words", slotLineWords)
		if err != nil {
			return err
		}
		var rects []rectF
		if words != nil {
			defer words.release()
			err = forEach(words, "IVectorView<OcrWord>", func(word *comObject) error {
				r, err := word.getRect("OcrWord.BoundingRect", slotWordBoundingRect)
				if err == nil {
					rects = append(rects, r)
				}
				return err
			})
			if err != nil {
				return err
			}
		}
		lines = append(lines, rawLine{text: text, words: rects})
		return nil
	})
	return lines, err
}

func recognizerTag(engine *comObject) (string, error) {
	lang, err := engine.getObject("OcrEngine.RecognizerLanguage", slotRecognizerLanguage)
	if err != nil {
		return "", err
	}
	if lang == nil {
		return "", &HResultError{Op: "OcrEngine.RecognizerLanguage", Code: uint32(windows.E_POINTER)}
	}
	defer lang.release()
	return lang.getString("Language.LanguageTag", slotLanguageTag)
}

// forEach calls fn with every non-null item of an IVectorView; each item is released after fn.
func forEach(view *comObject, op string, fn func(item *comObject) error) error {
	n, err := view.getUint32(op+".Size", slotSize)
	if err != nil {
		return err
	}
	for i := range n {
		item, err := view.getObjectWord(op+".GetAt", slotGetAt, uintptr(i))
		if err != nil {
			return err
		}
		if item == nil {
			continue
		}
		if err := withItem(item, fn); err != nil {
			return err
		}
	}
	return nil
}

// withItem releases item even when fn panics; the panic goes on to Reader's recover.
func withItem(item *comObject, fn func(*comObject) error) error {
	defer item.release()
	return fn(item)
}
