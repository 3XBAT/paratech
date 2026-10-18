package wails

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// The embedded icon is a valid ICO with the sizes the tray, the taskbar and the window need, and it is the same file as
// the installer's icon.
func TestAppIcon(t *testing.T) {
	icon := appIcon()
	if len(icon) < 6 || binary.LittleEndian.Uint16(icon[0:2]) != 0 || binary.LittleEndian.Uint16(icon[2:4]) != 1 {
		t.Fatal("the app icon is not an ICO file")
	}
	n := int(binary.LittleEndian.Uint16(icon[4:6]))
	sizes := map[int]bool{}
	for i := range n {
		entry := icon[6+16*i : 6+16*(i+1)]
		w := int(entry[0])
		if w == 0 {
			w = 256
		}
		size, off := binary.LittleEndian.Uint32(entry[8:12]), binary.LittleEndian.Uint32(entry[12:16])
		if uint64(off)+uint64(size) > uint64(len(icon)) {
			t.Fatalf("image %d lies outside the file", i)
		}
		sizes[w] = true
	}
	for _, want := range []int{16, 32, 48, 256} {
		if !sizes[want] {
			t.Errorf("the icon has no %dx%d image (sizes %v)", want, want, sizes)
		}
	}

	icon[0] = 0xFF
	if appIcon()[0] == 0xFF {
		t.Fatal("appIcon must return a copy")
	}

	installer, err := os.ReadFile(filepath.Join("..", "..", "..", "build", "windows", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installer, appIcon()) {
		t.Fatal("desktop/build/windows/icon.ico differs from the embedded icon: copy the same file to both")
	}
}
