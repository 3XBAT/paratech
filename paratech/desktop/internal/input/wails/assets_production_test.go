//go:build production

package wails

import (
	"io/fs"
	"testing"
)

// Release builds must ship the React app and its popup page, not the fallback pages: run `npm run build` in
// desktop/frontend first.
func TestProductionEmbedsTheBuiltApp(t *testing.T) {
	for _, page := range []string{"index.html", "popup.html"} {
		if _, err := fs.Stat(assetFiles, appDir+"/"+page); err != nil {
			t.Errorf("built %s missing (run npm run build in desktop/frontend): %v", page, err)
		}
	}
}
