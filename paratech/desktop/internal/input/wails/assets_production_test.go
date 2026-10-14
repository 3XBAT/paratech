//go:build production

package wails

import (
	"io/fs"
	"testing"
)

// Release builds must ship the React app, not the fallback page: run `npm run build` in desktop/frontend first.
func TestProductionEmbedsTheBuiltApp(t *testing.T) {
	if _, err := fs.Stat(assetFiles, appDir+"/index.html"); err != nil {
		t.Fatalf("built app missing (run npm run build in desktop/frontend): %v", err)
	}
}
