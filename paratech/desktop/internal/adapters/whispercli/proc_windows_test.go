//go:build windows

package whispercli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	const cpWestern = 1252

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

	err := f.eng.Transcribe(ctx, f.audio(t), model, outRef(t, "x.json"), func(int) { cancel() }, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	modelPath := filepath.Join(f.root, "models", model.Path())
	assertStopped(t, modelPath+beatSuffix)
	assertStopped(t, modelPath+childBeatSuffix)
	f.assertClean(t)
}

const (
	cpCyrillic = 1251
	cpJapanese = 932
)

func TestDecodeANSI(t *testing.T) {
	tests := []struct {
		name string
		cp   uint32
		in   string
	}{
		{"ascii", cpCyrillic, `C:\Users\Ivan`},
		{"cyrillic", cpCyrillic, `C:\Users\Иван Петров`},
		{"japanese", cpJapanese, `C:\Users\日本`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := tt.in
			if n, ok := nativeFormCP(tt.in, tt.cp); ok {
				raw = n
			}
			if got := decodeANSI([]byte(raw), tt.cp); got != tt.in {
				t.Errorf("decodeANSI = %q, want %q", got, tt.in)
			}
		})
	}

	// A lone lead byte is replaced, not an error.
	if got := decodeANSI([]byte("ok \x82"), cpJapanese); !strings.HasPrefix(got, "ok ") || !utf8.ValidString(got) {
		t.Errorf("decodeANSI with a lone lead byte = %q", got)
	}
}

// TestStderrTailANSIRoot uses the real code page conversion: a 1251 (or 932) root echoed by the CLI is redacted.
func TestStderrTailANSIRoot(t *testing.T) {
	tests := []struct {
		name   string
		cp     uint32
		root   string
		leaks  []string
		native bool
	}{
		{"cyrillic, decoded", cpCyrillic, `C:\Users\Иван Петров\AppData\Local\paratech`, []string{"иван", "петров"}, false},
		{"cyrillic, native and decoded", cpCyrillic, `C:\Users\Иван Петров\AppData\Local\paratech`, []string{"иван", "петров"}, true},
		{"japanese, native and decoded", cpJapanese, `C:\Users\日本\data`, []string{"日本"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := func(s string) string {
				n, ok := nativeFormCP(s, tt.cp)
				if !ok {
					t.Fatalf("%q not representable in %d", s, tt.cp)
				}
				return n
			}

			r := redaction{dir: tt.root, placeholder: placeholderData}
			if tt.native {
				r.native = []string{enc(tt.root), enc(strings.ReplaceAll(tt.root, `\`, "/"))}
			}

			s := newStderrSink(newReporter(nil, 0, time.Now))
			s.decode = func(b []byte) string { return decodeANSI(b, tt.cp) }
			_, _ = s.Write([]byte("loading '" + enc(tt.root) + `\models\m.bin'` + "\n" +
				"bad magic in " + enc(strings.ToUpper(tt.root)) + "\n"))
			got := s.tailText([]redaction{r})

			if !strings.Contains(got, `'<data>\models\m.bin'`) || !strings.Contains(got, "bad magic in <data>") {
				t.Errorf("tail %q not redacted", got)
			}
			for _, leak := range append(tt.leaks, enc(tt.leaks[0])) {
				if strings.Contains(strings.ToLower(got), leak) {
					t.Errorf("tail %q leaks %q", got, leak)
				}
			}
		})
	}
}

// TestKillRunExitedProcess: a process that has exited but is not waited yet is reported as done, so exec keeps
// its real exit status.
func TestKillRunExitedProcess(t *testing.T) {
	cmd := fakeCommand(t, filepath.Join(t.TempDir(), "missing.bin"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the fake to exit", func() bool { return processDone(cmd.Process.Pid) }, nil)

	if err := killRun(cmd.Process, nil); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killRun = %v, want os.ErrProcessDone", err)
	}
	if err := cmd.Wait(); cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != fakeExitNoModel {
		t.Errorf("Wait = %v, want exit code %d", err, fakeExitNoModel)
	}
}

func TestProcGroupNilIsSafe(t *testing.T) {
	var g *procGroup
	g.kill()
	g.close()
	if err := g.attach(1); err == nil {
		t.Error("attach on a nil group succeeded")
	}
}
