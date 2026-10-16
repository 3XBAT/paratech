//go:build windows

package urlscheme

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The real registry, below a random key of HKCU\Software (not Software\Classes, so no scheme is ever registered and
// the real paratech key is never touched); the key is deleted at the end.
func TestEnsureInTheRegistry(t *testing.T) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	root := `Software\paratech-test-` + hex.EncodeToString(b)
	t.Cleanup(func() {
		for _, k := range []string{`\shell\open\command`, `\shell\open`, `\shell`, ``} {
			if err := registry.DeleteKey(registry.CURRENT_USER, root+k); err != nil && !errors.Is(err, registry.ErrNotExist) {
				t.Errorf("delete %s: %v", root+k, err)
			}
		}
	})
	r := &Registrar{keys: platformKeys{}, root: root}
	const exe = `C:\path with space\Paratech.exe`

	for i, want := range []bool{true, false} {
		wrote, err := r.Ensure(exe)
		if err != nil || wrote != want {
			t.Fatalf("Ensure #%d = %v, %v; want %v", i+1, wrote, err, want)
		}
	}
	check := func(path, name, want string) {
		t.Helper()
		k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = k.Close() }()
		v, typ, err := k.GetStringValue(name)
		if err != nil || v != want || typ != registry.SZ {
			t.Fatalf("%s [%s] = %q (type %d), %v; want %q", path, name, v, typ, err, want)
		}
	}
	check(root, "", "URL:Paratech")
	check(root, "URL Protocol", "")
	check(root+`\shell\open\command`, "", `"C:\path with space\Paratech.exe" "%1"`)

	// Another exe, then an expandable string, are repaired.
	if wrote, err := r.Ensure(`D:\Other\Paratech.exe`); err != nil || !wrote {
		t.Fatalf("Ensure for another exe = %v, %v", wrote, err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	err = k.SetExpandStringValue("", `"C:\path with space\Paratech.exe" "%1"`)
	_ = k.Close()
	if err != nil {
		t.Fatal(err)
	}
	if wrote, err := r.Ensure(exe); err != nil || !wrote {
		t.Fatalf("Ensure over REG_EXPAND_SZ = %v, %v", wrote, err)
	}
	check(root+`\shell\open\command`, "", `"C:\path with space\Paratech.exe" "%1"`)
}
