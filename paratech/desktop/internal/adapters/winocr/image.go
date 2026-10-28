package winocr

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"paratech/desktop/internal/domain/library/material"
)

// Image formats accepted by Read, as named by image.DecodeConfig.
const (
	formatPNG  = "png"
	formatJPEG = "jpeg"
)

// header is what is read of an image before it is decoded.
type header struct {
	format        string
	width, height int
}

// checkImage reads the header only and refuses what is not a PNG or JPEG or is over the photo limits, so a
// decompression bomb is never decoded.
func checkImage(data []byte) (header, error) {
	switch {
	case len(data) == 0:
		return header{}, fmt.Errorf("%w: empty", ErrBadImage)
	case len(data) > material.MaxImageBytes:
		return header{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(data))
	}
	var (
		cfg image.Config
		err error
		h   header
	)
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		h.format = formatPNG
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case bytes.HasPrefix(data, []byte("\xff\xd8")):
		h.format = formatJPEG
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	default:
		return header{}, fmt.Errorf("%w: not a PNG or JPEG", ErrBadImage)
	}
	if err != nil {
		return header{}, fmt.Errorf("%w: header: %v", ErrBadImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return header{}, fmt.Errorf("%w: %dx%d", ErrBadImage, cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > material.MaxImagePixels {
		return header{}, fmt.Errorf("%w: %dx%d", ErrTooLarge, cfg.Width, cfg.Height)
	}
	h.width, h.height = cfg.Width, cfg.Height
	return h, nil
}

// decodeGray decodes the image checked by checkImage into 8-bit grey: the engine reads luminance, and a grey
// bitmap takes a quarter of the memory of BGRA.
func decodeGray(data []byte, h header) (*image.Gray, error) {
	var (
		img image.Image
		err error
	)
	if h.format == formatJPEG {
		img, err = jpeg.Decode(bytes.NewReader(data))
	} else {
		img, err = png.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadImage, err)
	}
	if b := img.Bounds(); b.Dx() != h.width || b.Dy() != h.height {
		return nil, fmt.Errorf("%w: decoded %dx%d, header %dx%d", ErrBadImage, b.Dx(), b.Dy(), h.width, h.height)
	}
	return toGray(img), nil
}

// toGray converts to grey with the luma weights of color.GrayModel; transparent pixels are laid over white, as on
// paper. The common decoder outputs have fast paths.
func toGray(img image.Image) *image.Gray {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	switch src := img.(type) {
	case *image.Gray:
		for y := range h {
			copy(dst.Pix[y*dst.Stride:y*dst.Stride+w], src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):])
		}
	case *image.YCbCr: // JPEG: Y is the luma already
		for y := range h {
			copy(dst.Pix[y*dst.Stride:y*dst.Stride+w], src.Y[src.YOffset(b.Min.X, b.Min.Y+y):])
		}
	case *image.NRGBA:
		for y := range h {
			row := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):]
			for x := range w {
				p := row[4*x : 4*x+4]
				a := uint32(p[3])
				r, g, bl := over(uint32(p[0])*a/0xff, a), over(uint32(p[1])*a/0xff, a), over(uint32(p[2])*a/0xff, a)
				dst.Pix[y*dst.Stride+x] = luma(r, g, bl)
			}
		}
	case *image.RGBA:
		for y := range h {
			row := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):]
			for x := range w {
				p := row[4*x : 4*x+4]
				a := uint32(p[3])
				dst.Pix[y*dst.Stride+x] = luma(over(uint32(p[0]), a), over(uint32(p[1]), a), over(uint32(p[2]), a))
			}
		}
	default:
		for y := range h {
			for x := range w {
				r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA() // 16-bit, premultiplied
				dst.Pix[y*dst.Stride+x] = luma(over16(r, a), over16(g, a), over16(bl, a))
			}
		}
	}
	return dst
}

// over lays a premultiplied 8-bit channel with alpha a over white.
func over(c, a uint32) uint32 { return min(c+0xff-a, 0xff) }

// over16 is over for 16-bit channels; it returns 8 bits.
func over16(c, a uint32) uint32 { return min(c+0xffff-a, 0xffff) >> 8 }

// luma is the 8-bit grey of color.GrayModel for 8-bit r, g, b.
func luma(r, g, b uint32) uint8 {
	return uint8((19595*r + 38470*g + 7471*b + 1<<15) >> 16) //nolint:gosec // weights sum to 1<<16, so the result fits 8 bits
}

// fit downscales g, keeping the aspect ratio, until neither side is over maxDim; maxDim <= 0 means no limit.
func fit(g *image.Gray, maxDim int) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	if maxDim <= 0 || w <= maxDim && h <= maxDim {
		return g
	}
	nw, nh := maxDim, maxDim
	if w >= h {
		nh = max(1, int(int64(h)*int64(maxDim)/int64(w)))
	} else {
		nw = max(1, int(int64(w)*int64(maxDim)/int64(h)))
	}
	return resize(g, nw, nh)
}

// resize downscales g to nw×nh (each not larger than g) by averaging the source pixels of every target pixel's box.
func resize(g *image.Gray, nw, nh int) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	dst := image.NewGray(image.Rect(0, 0, nw, nh))
	acc := make([]uint32, w)
	for dy := range nh {
		y0, y1 := span(dy, h, nh)
		clear(acc)
		for y := y0; y < y1; y++ {
			row := g.Pix[g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y):]
			for x := range w {
				acc[x] += uint32(row[x])
			}
		}
		for dx := range nw {
			x0, x1 := span(dx, w, nw)
			var sum uint64
			for x := x0; x < x1; x++ {
				sum += uint64(acc[x])
			}
			n := uint64((x1 - x0) * (y1 - y0))                 //nolint:gosec // positive: span returns a non-empty range
			dst.Pix[dy*dst.Stride+dx] = uint8((sum + n/2) / n) //nolint:gosec // an average of 8-bit values
		}
	}
	return dst
}

// span is the source range [lo, hi) of target index i when n source pixels shrink to m (m <= n); never empty.
func span(i, n, m int) (lo, hi int) {
	lo = i * n / m
	hi = max((i+1)*n/m, lo+1)
	return lo, hi
}
