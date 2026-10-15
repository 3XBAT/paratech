//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
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

// Every Windows function the enumeration looks up by name exists: a wrong name would only show as empty titles or
// no icons.
func TestAppsProcsExist(t *testing.T) {
	w, ok := platformApps().(*winApps)
	if !ok {
		t.Fatal("platformApps is not the Windows implementation")
	}
	for _, p := range []*windows.LazyProc{
		w.getWindowText, w.getWindowLongPtrW, w.getIconInfo, w.destroyIcon, w.extractIconExW, w.getObjectW,
		w.getDIBits, w.createCompatibleDC, w.deleteDC, w.deleteObject,
	} {
		if err := p.Find(); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
	}
}

// imageName starts small and grows the buffer only while Windows reports it too small, up to the longest path.
func TestImageName(t *testing.T) {
	denied := windows.ERROR_ACCESS_DENIED
	tests := []struct {
		name  string
		chars uint32 // the length of the name; 0: query fails with err
		err   error
		want  string
		sizes []uint32
	}{
		{"short", 20, nil, strings.Repeat("a", 20), []uint32{1024}},
		{"long", 5000, nil, strings.Repeat("a", 5000), []uint32{1024, 4096, 16384}},
		{"longest", maxImageChars - 1, nil, strings.Repeat("a", maxImageChars-1), []uint32{1024, 4096, 16384, 32768}},
		{"too long", maxImageChars + 1, windows.ERROR_INSUFFICIENT_BUFFER, "", []uint32{1024, 4096, 16384, 32768}},
		{"other error", 0, denied, "", []uint32{1024}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sizes []uint32
			got, err := imageName(func(buf *uint16, n *uint32) error {
				sizes = append(sizes, *n)
				switch {
				case tt.chars == 0:
					return denied
				case tt.chars >= *n: // no room for the terminating NUL
					return windows.ERROR_INSUFFICIENT_BUFFER
				}
				out := unsafe.Slice(buf, *n)
				for i := range tt.chars {
					out[i] = 'a'
				}
				out[tt.chars], *n = 0, tt.chars
				return nil
			})
			if got != tt.want || !errors.Is(err, tt.err) || !slices.Equal(sizes, tt.sizes) {
				t.Fatalf("imageName = %d chars, %v; buffers %v; want %d chars, %v; buffers %v",
					len(got), err, sizes, len(tt.want), tt.err, tt.sizes)
			}
		})
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
