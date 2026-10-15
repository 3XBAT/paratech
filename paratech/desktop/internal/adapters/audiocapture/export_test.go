package audiocapture

import (
	"bytes"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// Exports for the external tests of this package.

// TestStore is the directory-backed Storage Manager of the tests.
type TestStore = dirStore

// NewTestStore returns a TestStore on root, laid out like the filestore adapter (<root>/<kind>/<path>).
func NewTestStore(root string) *TestStore { return &dirStore{root: root} }

// Names lists the file names in dir.
func (d *dirStore) Names(t *testing.T, dir shared.ArtifactRef) []string { return d.names(t, dir) }

// Get returns the content of a file.
func (d *dirStore) Get(t *testing.T, ref shared.ArtifactRef) []byte { return d.get(t, ref) }

// ReadWAV checks a complete WAV file and returns its format, frame count and samples.
func ReadWAV(b []byte) (Format, int64, []byte, error) {
	info, err := readHeader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return Format{}, 0, nil, err
	}
	data := b[headerLen(info.Format):]
	return info.Format, info.Frames, data[:info.Frames*int64(info.Format.BlockAlign())], nil
}

// EncodeRequest returns the start request line of a recorder child.
func EncodeRequest(r Request) ([]byte, error) { return encodeRequest(r) }
