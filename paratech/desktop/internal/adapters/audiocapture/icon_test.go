package audiocapture

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// bgra builds a width x height bitmap of one color.
func bgra(width, height int, b, g, r, a byte) []byte {
	out := make([]byte, 0, width*height*4)
	for range width * height {
		out = append(out, b, g, r, a)
	}
	return out
}

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	if b == nil {
		t.Fatal("no PNG")
	}
	if len(b) > maxIconBytes {
		t.Fatalf("PNG of %d bytes", len(b))
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func TestIconPNG(t *testing.T) {
	t.Run("with alpha", func(t *testing.T) {
		px := bgra(32, 32, 0x10, 0x20, 0x30, 0x80)
		img := decodePNG(t, iconPNG(32, 32, px, nil))
		if img.Bounds().Dx() != 32 || nrgbaAt(img, 5, 5) != (color.NRGBA{R: 0x30, G: 0x20, B: 0x10, A: 0x80}) {
			t.Fatalf("size %v, pixel %v", img.Bounds(), nrgbaAt(img, 5, 5))
		}
	})
	t.Run("transparency from the mask", func(t *testing.T) {
		px := bgra(16, 16, 0x10, 0x20, 0x30, 0)
		mask := bgra(16, 16, 0, 0, 0, 0)
		copy(mask[:4], []byte{0xff, 0xff, 0xff, 0}) // the first pixel is transparent
		img := decodePNG(t, iconPNG(16, 16, px, mask))
		if nrgbaAt(img, 0, 0).A != 0 || nrgbaAt(img, 1, 0).A != 0xff {
			t.Fatalf("alpha %d and %d, want 0 and 255", nrgbaAt(img, 0, 0).A, nrgbaAt(img, 1, 0).A)
		}
	})
	t.Run("no alpha and no mask is opaque", func(t *testing.T) {
		img := decodePNG(t, iconPNG(8, 8, bgra(8, 8, 1, 2, 3, 0), nil))
		if nrgbaAt(img, 7, 7).A != 0xff {
			t.Fatal("not opaque")
		}
	})
	t.Run("large icons shrink", func(t *testing.T) {
		img := decodePNG(t, iconPNG(64, 64, bgra(64, 64, 0, 0, 0xff, 0xff), nil))
		if img.Bounds().Dx() != iconSize || img.Bounds().Dy() != iconSize || nrgbaAt(img, 10, 10) != (color.NRGBA{R: 0xff, A: 0xff}) {
			t.Fatalf("size %v, pixel %v", img.Bounds(), nrgbaAt(img, 10, 10))
		}
	})
	t.Run("wide icons keep their aspect", func(t *testing.T) {
		img := decodePNG(t, iconPNG(64, 32, bgra(64, 32, 0, 0xff, 0, 0xff), nil))
		if img.Bounds().Dx() != iconSize || img.Bounds().Dy() != iconSize/2 {
			t.Fatalf("size %v", img.Bounds())
		}
	})
	t.Run("transparent pixels do not darken a shrunk icon", func(t *testing.T) {
		px := bgra(64, 64, 0, 0, 0xff, 0xff)
		for i := 0; i < len(px); i += 8 { // every other pixel transparent black
			copy(px[i:i+4], []byte{0, 0, 0, 0})
		}
		img := decodePNG(t, iconPNG(64, 64, px, nil))
		if c := nrgbaAt(img, 3, 3); c.R != 0xff || c.A != 0x7f {
			t.Fatalf("pixel %v, want pure red at half alpha", c)
		}
	})
	for name, f := range map[string]func() []byte{
		"zero size":       func() []byte { return iconPNG(0, 32, nil, nil) },
		"negative size":   func() []byte { return iconPNG(-1, 32, nil, nil) },
		"too large":       func() []byte { return iconPNG(maxIconInput+1, 1, make([]byte, (maxIconInput+1)*4), nil) },
		"short pixels":    func() []byte { return iconPNG(32, 32, make([]byte, 10), nil) },
		"pixels too long": func() []byte { return iconPNG(1, 1, make([]byte, 8), nil) },
	} {
		t.Run(name, func(t *testing.T) {
			if b := f(); b != nil {
				t.Fatalf("iconPNG = %d bytes, want nil", len(b))
			}
		})
	}
}
