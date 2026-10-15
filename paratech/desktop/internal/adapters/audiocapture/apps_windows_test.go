//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"strings"
	"testing"
	"unsafe"
)

// The Go mirrors of the icon structures have the 64-bit ABI layout.
func TestIconStructLayout(t *testing.T) {
	var (
		ii iconInfo
		bm bitmapObject
		bi bitmapInfo
	)
	tests := []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(ICONINFO)", unsafe.Sizeof(ii), 32},
		{"ICONINFO.hbmMask", unsafe.Offsetof(ii.hbmMask), 16},
		{"ICONINFO.hbmColor", unsafe.Offsetof(ii.hbmColor), 24},
		{"sizeof(BITMAP)", unsafe.Sizeof(bm), 32},
		{"BITMAP.bmBitsPixel", unsafe.Offsetof(bm.bmBitsPixel), 18},
		{"BITMAP.bmBits", unsafe.Offsetof(bm.bmBits), 24},
		{"sizeof(BITMAPINFOHEADER)", unsafe.Offsetof(bi.colors), 40},
		{"BITMAPINFOHEADER.biCompression", unsafe.Offsetof(bi.compression), 16},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestSessionGUIDs(t *testing.T) {
	for name, tt := range map[string]struct{ got, want string }{
		"IID_IAudioSessionManager2": {iidIAudioSessionManager2().String(), "{77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F}"},
		"IID_IAudioSessionControl2": {iidIAudioSessionControl2().String(), "{BFB7FF88-7239-4FC9-8FA2-07C950BE9C6D}"},
		"IID_ISimpleAudioVolume":    {iidISimpleAudioVolume().String(), "{87CE5498-68D6-44E5-9215-6DA47EF883D8}"},
	} {
		if got := strings.ToUpper(tt.got); got != tt.want {
			t.Errorf("%s = %s, want %s", name, got, tt.want)
		}
	}
}
