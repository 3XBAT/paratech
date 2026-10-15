package audiocapture

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// Icon limits of the source picker.
const (
	iconSize     = 32       // the PNG is at most iconSize x iconSize
	maxIconBytes = 16 << 10 // a larger PNG is dropped
	maxIconInput = 256      // a larger bitmap is not read
)

// iconPNG converts a top-down 32-bit BGRA icon bitmap into a PNG of at most iconSize pixels a side. An icon without
// alpha (all zero) takes its transparency from mask, the AND mask read as 32-bit BGRA (white = transparent). It
// returns nil for malformed input.
func iconPNG(width, height int, bgra, mask []byte) []byte {
	if width <= 0 || height <= 0 || width > maxIconInput || height > maxIconInput || len(bgra) != width*height*4 {
		return nil
	}
	hasAlpha := false
	for i := 3; i < len(bgra); i += 4 {
		if bgra[i] != 0 {
			hasAlpha = true
			break
		}
	}
	useMask := !hasAlpha && len(mask) == len(bgra)

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		b, g, r, a := bgra[4*i], bgra[4*i+1], bgra[4*i+2], bgra[4*i+3]
		if !hasAlpha {
			a = 0xff
			if useMask && mask[4*i] != 0 {
				a = 0
			}
		}
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = r, g, b, a
	}
	if width > iconSize || height > iconSize {
		img = shrink(img, iconSize)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil || buf.Len() > maxIconBytes {
		return nil
	}
	return buf.Bytes()
}

// shrink scales src down to fit size x size by averaging the covered pixels, weighted by alpha so that transparent
// pixels do not darken the edges.
func shrink(src *image.NRGBA, size int) *image.NRGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := size, size
	if sw > sh {
		dh = max(1, sh*size/sw)
	} else if sh > sw {
		dw = max(1, sw*size/sh)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0, y1 := y*sh/dh, max((y+1)*sh/dh, y*sh/dh+1)
		for x := range dw {
			x0, x1 := x*sw/dw, max((x+1)*sw/dw, x*sw/dw+1)
			var r, g, b, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := src.NRGBAAt(sx, sy)
					w := uint64(c.A)
					r, g, b, a, n = r+uint64(c.R)*w, g+uint64(c.G)*w, b+uint64(c.B)*w, a+w, n+1
				}
			}
			if a == 0 {
				continue // transparent
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r / a), G: uint8(g / a), B: uint8(b / a), A: uint8(a / n)}) //nolint:gosec // averages of 8-bit values
		}
	}
	return dst
}
