package tests

import (
	"fmt"
	"go/parser"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toastPackages show Windows toasts. Finished processing notifies nobody (TASK-065, owner decision): the user follows
// it on the recordings and jobs page, and the lesson-started popup is the only notification. A finished job can only
// reach a toast through one of these packages, so no desktop file may import them.
var toastPackages = []string{
	"github.com/wailsapp/wails/v3/pkg/services/notifications",
	"git.sr.ht/~jackmordaunt/go-toast",
}

// toastImports lists the .go files below root (every build tag: go/parser ignores them) that import a toast package.
func toastImports(root string) ([]string, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range files {
		f, err := parseFile(root, rel, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, imp := range importPaths(f) {
			for _, p := range toastPackages {
				if imp == p || strings.HasPrefix(imp, p+"/") {
					out = append(out, violation("TASK-065", rel, fmt.Sprintf("imports %q: no completion toasts", imp)))
				}
			}
		}
	}
	return out, nil
}

func TestNoCompletionToasts(t *testing.T) {
	t.Run("no toast package in the tree", func(t *testing.T) {
		v, err := toastImports(moduleRoot(t))
		report(t, v, err)
	})
	t.Run("no toast module in go.mod", func(t *testing.T) {
		mod, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(mod), "go-toast") {
			t.Error("go.mod requires go-toast: something shows Windows toasts again")
		}
	})
	t.Run("the check finds toast imports", func(t *testing.T) {
		root := writeTree(t, map[string]string{
			"internal/input/wails/toasts_windows.go": "//go:build windows\n\n" +
				goSrc("github.com/wailsapp/wails/v3/pkg/services/notifications"),
			"internal/app/toast.go":  goSrc("git.sr.ht/~jackmordaunt/go-toast/v2"),
			"internal/app/window.go": goSrc("github.com/wailsapp/wails/v3/pkg/application"),
		})
		v, err := toastImports(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 2 {
			t.Fatalf("violations %v, want the two toast imports", v)
		}
	})
}
