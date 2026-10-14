package tests

// TASK-029 test step 3, AC-06, hard rule 2: the STT path makes no network calls. Starting from the packages that take
// part in a transcription, every package of this module they import (directly or through other module packages) is
// checked, for Windows and for Linux build tags, and none may import a network package. Third-party packages are not
// walked: golang.org/x/sys/windows imports net for socket types but opens no connection.

import (
	"errors"
	"go/build"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sttPathRoots are the packages of a transcription: the use case, the engine adapter, the file store and the database.
var sttPathRoots = []string{
	"internal/cases/transcribe",
	"internal/adapters/whispercli",
	"internal/adapters/filestore",
	"internal/adapters/sqlite",
}

// sttPathGOOS are the platforms whose build tags are checked: the product target and the CI platform.
var sttPathGOOS = []string{"windows", "linux"}

// pureNetPackages are parsers below net/ that open no connection.
var pureNetPackages = map[string]bool{"net/url": true, "net/netip": true}

// networkPrefixes are the client libraries that reach the network (the Hub contracts included).
var networkPrefixes = []string{
	"crypto/tls", "connectrpc.com/", "google.golang.org/grpc", "google.golang.org/api", "golang.org/x/net/",
	"golang.org/x/oauth2", "paratech/shared/",
}

func isNetworkImport(imp string) bool {
	if imp == "net" || (strings.HasPrefix(imp, "net/") && !pureNetPackages[imp]) {
		return true
	}
	for _, p := range networkPrefixes {
		if imp == strings.TrimSuffix(p, "/") || strings.HasPrefix(imp, p) {
			return true
		}
	}
	return false
}

// sttNetworkViolations walks the module packages reachable from roots (paths relative to the module root) with the
// build tags of goos and reports every network import of their non-test files, and the packages it visited.
func sttNetworkViolations(root, modPath, goos string, roots []string) (violations []string, visited map[string]bool, err error) {
	ctxt := build.Default
	ctxt.GOOS = goos
	ctxt.CgoEnabled = false

	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		if seen[rel] {
			continue
		}
		seen[rel] = true

		pkg, err := ctxt.ImportDir(filepath.Join(root, filepath.FromSlash(rel)), 0)
		var noGo *build.NoGoError
		if errors.As(err, &noGo) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}

		for _, imp := range pkg.Imports {
			if isNetworkImport(imp) {
				violations = append(violations, rel+" imports "+imp+" (GOOS="+goos+")")
			}
			if sub, ok := strings.CutPrefix(imp, modPath+"/"); ok {
				queue = append(queue, sub)
			}
		}
	}
	sort.Strings(violations)
	return violations, seen, nil
}

func TestSTTPathHasNoNetworkImports(t *testing.T) {
	root := moduleRoot(t)
	for _, goos := range sttPathGOOS {
		v, visited, err := sttNetworkViolations(root, modulePath, goos, sttPathRoots)
		if err != nil {
			t.Fatalf("GOOS=%s: %v", goos, err)
		}
		// The walk must reach the domain behind the roots, or the check proves nothing.
		for _, pkg := range []string{"internal/domain/transcription/transcript", "internal/domain/jobs/job", "internal/domain/shared"} {
			if !visited[pkg] {
				t.Errorf("GOOS=%s: the walk did not reach %s", goos, pkg)
			}
		}
		for _, msg := range v {
			t.Errorf("AC-06 / hard rule 2: the STT path must not reach the network: %s", msg)
		}
	}
}

func TestSTTNetworkCheckDetectsViolations(t *testing.T) {
	const m = modulePath
	tree := writeTree(t, map[string]string{
		"internal/cases/transcribe/a.go":       goSrc("io", m+"/internal/domain/transcription/transcript"),
		"internal/cases/transcribe/a_test.go":  goSrc("net/http"), // tests may use the network
		"internal/domain/transcription/t/t.go": goSrc("net/url"),
		"internal/domain/transcription/transcript/t.go": goSrc(
			m+"/internal/domain/transcription/t", "net/http"),
		"internal/adapters/whispercli/w.go":         goSrc("os/exec"),
		"internal/adapters/whispercli/w_windows.go": "//go:build windows\n\n" + goSrc("net"),
		"internal/adapters/other/o.go":              goSrc("connectrpc.com/connect"), // not reachable from the roots
	})
	roots := []string{"internal/cases/transcribe", "internal/adapters/whispercli"}

	tests := []struct {
		goos string
		want []string
	}{
		{"windows", []string{
			"internal/adapters/whispercli imports net (GOOS=windows)",
			"internal/domain/transcription/transcript imports net/http (GOOS=windows)",
		}},
		{"linux", []string{
			"internal/domain/transcription/transcript imports net/http (GOOS=linux)",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			got, _, err := sttNetworkViolations(tree, m, tc.goos, roots)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestIsNetworkImport(t *testing.T) {
	for imp, want := range map[string]bool{
		"net": true, "net/http": true, "net/http/httputil": true, "net/smtp": true, "net/rpc": true, "crypto/tls": true,
		"connectrpc.com/connect": true, "google.golang.org/grpc": true, "paratech/shared/gen/hub/v1": true,
		"golang.org/x/net/http2": true,
		"net/url":                false, "net/netip": false, "os/exec": false, "io": false, "netstat": false,
		"golang.org/x/sys/windows": false, "modernc.org/sqlite": false, "crypto/sha256": false,
	} {
		if got := isNetworkImport(imp); got != want {
			t.Errorf("isNetworkImport(%q) = %v, want %v", imp, got, want)
		}
	}
}
