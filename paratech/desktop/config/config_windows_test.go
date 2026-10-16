//go:build windows

package config_test

import (
	"io"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"paratech/desktop/config"
)

// The command line the registered command "<exe>" "%1" makes for a link with a '"' splits into several arguments;
// Load still ignores all of them.
func TestSchemeLinkWithAQuoteSplits(t *testing.T) {
	const line = `"C:\Program Files\Paratech\Paratech.exe" "paratech://x" --data-dir "C:\evil" --headless`
	// shell32's parser; Go's own one for os.Args follows the same rules here. The few bytes it allocates are not freed.
	var argc int32
	argv, err := syscall.CommandLineToArgv(syscall.StringToUTF16Ptr(line), &argc)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, p := range argv[:argc] {
		args = append(args, syscall.UTF16ToString(p[:]))
	}
	want := []string{`C:\Program Files\Paratech\Paratech.exe`, "paratech://x", "--data-dir", `C:\evil`, "--headless"}
	if !slices.Equal(args, want) {
		t.Fatalf("argv %q, want %q", args, want)
	}

	abs := filepath.Join(t.TempDir(), "data")
	got, err := config.Load(args[1:], io.Discard, func(k string) string { return map[string]string{config.EnvDataDir: abs}[k] })
	if err != nil || got.DataDir != abs || got.Headless {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}
