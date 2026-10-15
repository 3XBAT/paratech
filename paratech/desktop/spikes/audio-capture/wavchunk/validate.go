package wavchunk

import (
	"fmt"
	"os"
	"strings"
)

// Result is the validation outcome of one chunk file.
type Result struct {
	Name string
	Info Info  // valid when Err is nil
	Err  error // wraps ErrIncomplete or ErrNotWAV for a chunk that cannot be read as audio
}

// Partial reports whether the file still has its temporary name (the writer did not finish it).
func (r Result) Partial() bool { return strings.HasSuffix(r.Name, partialSuffix) }

// ValidateDir validates every chunk file (final and temporary) in dir, in name order. Only names the writer
// generates are opened, and only inside dir.
func ValidateDir(dir string) ([]Result, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("wavchunk: %s: %w", opOpenDir, err)
	}
	defer func() { _ = root.Close() }()

	names, err := chunkNames(root)
	if err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(names))
	for _, name := range names {
		info, err := validateFile(root, name)
		out = append(out, Result{Name: name, Info: info, Err: err})
	}
	return out, nil
}

func validateFile(root *os.Root, name string) (Info, error) {
	f, err := root.Open(name)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	return Read(f, st.Size())
}
