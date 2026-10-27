package speechcomponent

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

const vadURL = "https://models.example.org/vad.bin"

var vadBytes = []byte("voice activity model")

// vadSpec is testSpec with a VAD model downloaded on its own.
func vadSpec(t *testing.T) values.SpeechComponent {
	t.Helper()
	doc := fmt.Sprintf(`{"schema_version": 1, "version": "b1", "source_url": "https://example.com/r/archive.zip",
 "size_bytes": %d, "sha256": %q, "download_hosts": ["cdn.example.com"], "entry_prefix": "Release/",
 "executable": "whisper-cli.exe", "vad_model": "vad.bin", "files": [
 {"name": "whisper-cli.exe", "size_bytes": %d, "sha256": %q},
 {"name": "whisper.dll", "size_bytes": %d, "sha256": %q},
 {"name": "vad.bin", "size_bytes": %d, "sha256": %q, "source_url": %q}]}`,
		len(archiveBytes), digest(archiveBytes),
		len(testFiles["whisper-cli.exe"]), digest(testFiles["whisper-cli.exe"]),
		len(testFiles["whisper.dll"]), digest(testFiles["whisper.dll"]),
		len(vadBytes), digest(vadBytes), vadURL)
	spec, err := values.ParseSpeechComponent([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func vadSet() []values.FoundFile {
	return append(verifiedSet(), values.FoundFile{Name: "vad.bin", Regular: true, Size: int64(len(vadBytes)), SHA256: digest(vadBytes)})
}

func TestInstallDownloadsTheSeparateFiles(t *testing.T) {
	tests := []struct {
		name     string
		served   []byte
		want     State
		wantErr  error
		unpacked int
	}{
		{"verified", vadBytes, StateReady, nil, 1},
		{"other content", []byte("voice activity modeX"), StateFailed, values.ErrChecksumMismatch, 0},
		{"short", vadBytes[:5], StateFailed, values.ErrSizeMismatch, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, func(e *env) {
				e.spec = vadSpec(t)
				e.source.others = map[string][]byte{vadURL: tt.served}
				e.files.unpackSet = vadSet()
			})
			st := e.installed()
			if st.State != tt.want || (tt.wantErr != nil && !errors.Is(st.Err, tt.wantErr)) {
				t.Fatalf("status = %+v, want %s with %v", st, tt.want, tt.wantErr)
			}
			if got := e.source.urls; !slices.Equal(got, []string{e.spec.SourceURL().String(), vadURL}) {
				t.Fatalf("fetched %v, want the archive, then the VAD model", got)
			}
			if e.source.allow.Check(vadURL) != nil {
				t.Fatal("the source host of the separate file must be allowed")
			}
			unpacked, discarded := e.files.counts()
			if unpacked != tt.unpacked || discarded != 1 {
				t.Fatalf("unpacked %d, discarded %d; want %d, 1", unpacked, discarded, tt.unpacked)
			}
		})
	}
}
