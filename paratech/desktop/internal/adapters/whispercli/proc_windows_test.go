//go:build windows

package whispercli

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"paratech/desktop/testenv"
)

// makeDirLink creates a junction: unlike a symlink it needs no privilege on Windows.
func makeDirLink(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction: %v: %s", err, out)
	}
}

func TestCheckCodePage(t *testing.T) {
	const (
		cpWestern  = 1252
		cpCyrillic = 1251
		cpJapanese = 932
	)

	tests := []struct {
		name string
		cp   uint32
		s    string
		ok   bool
	}{
		{"ascii", cpWestern, `C:\Users\Ivan\data`, true},
		{"latin in western", cpWestern, `C:\Users\José\data`, true},
		{"cyrillic in western", cpWestern, `C:\Users\Иван\data`, false},
		{"cyrillic in cyrillic", cpCyrillic, `C:\Users\Иван\data`, true},
		{"fullwidth quote best-fits to a quote", cpWestern, `C:\a＂ -m x`, false},
		{"fullwidth backslash", cpWestern, `C:\a＼b`, false},
		{"emoji", cpCyrillic, `C:\a✓b`, false},
		{"japanese without a backslash trail byte", cpJapanese, `C:\日本`, true},
		{"japanese trail byte 0x5C", cpJapanese, `C:\表`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCodePage(tt.s, tt.cp)
			if tt.ok && err != nil {
				t.Fatalf("checkCodePage: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}

	if err := checkArgEncoding(`C:\plain\ascii path`); err != nil {
		t.Errorf("ASCII rejected under the system code page: %v", err)
	}
}

func TestBinaryNameOK(t *testing.T) {
	for name, want := range map[string]bool{
		`C:\w\whisper-cli.exe`: true,
		`C:\w\WHISPER-CLI.EXE`: true,
		`C:\w\whisper-cli.bat`: false,
		`C:\w\whisper-cli.cmd`: false,
		`C:\w\whisper-cli`:     false,
	} {
		if got := binaryNameOK(name); got != want {
			t.Errorf("binaryNameOK(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestTranscribeCancelKillsProcessTree checks the job object: a grandchild of the process dies too.
func TestTranscribeCancelKillsProcessTree(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	model := f.model(t, modeTree)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := f.eng.Transcribe(ctx, f.audio(t), model, outRef(t, "x.json"), func(int) { cancel() })

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	modelPath := filepath.Join(f.root, "models", model.Path())
	assertStopped(t, modelPath+beatSuffix)
	assertStopped(t, modelPath+childBeatSuffix)
	f.assertClean(t)
}

func TestProcGroupNilIsSafe(t *testing.T) {
	var g *procGroup
	g.kill()
	g.close()
	if err := g.attach(1); err == nil {
		t.Error("attach on a nil group succeeded")
	}
}
