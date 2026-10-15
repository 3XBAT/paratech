//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// iconInfo mirrors ICONINFO (winuser.h): BOOL, two DWORDs, then two HBITMAPs (32 bytes on 64-bit, checked by a test).
type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  windows.Handle
	hbmColor windows.Handle
}

// bitmapObject mirrors BITMAP (wingdi.h): four LONGs, two WORDs, then the bits pointer (32 bytes on 64-bit).
type bitmapObject struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

// bitmapInfo mirrors BITMAPINFOHEADER (40 bytes) followed by room for a color table, which GetDIBits may write.
type bitmapInfo struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
	colors        [256]uint32
}

const (
	biRGB        = 0 // BI_RGB
	dibRGBColors = 0 // DIB_RGB_COLORS
)

// icon extracts the large icon of the executable (32 x 32 at 100 % scale) and converts it with iconPNG. Every
// handle is released; any failure gives nil.
func (w *winApps) icon(path string) []byte {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var large windows.Handle
	n, _, _ := w.extractIconExW.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&large)), 0, 1) //nolint:gosec // G103: path in, icon handle out
	if n == 0 || large == 0 {
		return nil
	}
	defer w.destroyIcon.Call(uintptr(large)) //nolint:errcheck // nothing to do when it fails

	var ii iconInfo
	if r, _, _ := w.getIconInfo.Call(uintptr(large), uintptr(unsafe.Pointer(&ii))); r == 0 { //nolint:gosec // G103: ICONINFO out-parameter
		return nil
	}
	defer func() {
		for _, h := range []windows.Handle{ii.hbmMask, ii.hbmColor} {
			if h != 0 {
				_, _, _ = w.deleteObject.Call(uintptr(h))
			}
		}
	}()
	if ii.hbmColor == 0 {
		return nil // a monochrome icon: rare, shown without an icon
	}

	var bm bitmapObject
	if r, _, _ := w.getObjectW.Call(uintptr(ii.hbmColor), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 { //nolint:gosec // G103: BITMAP out-parameter
		return nil
	}
	width, height := int(bm.bmWidth), int(bm.bmHeight)
	if width <= 0 || height <= 0 || width > maxIconInput || height > maxIconInput {
		return nil
	}

	dc, _, _ := w.createCompatibleDC.Call(0)
	if dc == 0 {
		return nil
	}
	defer w.deleteDC.Call(dc) //nolint:errcheck // nothing to do when it fails

	color := w.dibits(dc, ii.hbmColor, width, height)
	if color == nil {
		return nil
	}
	var mask []byte
	if ii.hbmMask != 0 {
		mask = w.dibits(dc, ii.hbmMask, width, height)
	}
	return iconPNG(width, height, color, mask)
}

// dibits reads a bitmap as top-down 32-bit BGRA rows; nil on failure.
func (w *winApps) dibits(dc uintptr, bitmap windows.Handle, width, height int) []byte {
	bi := &bitmapInfo{width: int32(width), height: -int32(height), planes: 1, bitCount: 32, compression: biRGB} //nolint:gosec // bounded by maxIconInput
	bi.size = uint32(unsafe.Offsetof(bi.colors))
	buf := make([]byte, width*height*4)
	r, _, _ := w.getDIBits.Call(dc, uintptr(bitmap), 0, uintptr(height), uintptr(unsafe.Pointer(&buf[0])), //nolint:gosec // G103: pixel buffer
		uintptr(unsafe.Pointer(bi)), dibRGBColors) //nolint:gosec // G103: BITMAPINFO in and out
	if int(r) != height { //nolint:gosec // a line count
		return nil
	}
	return buf
}
