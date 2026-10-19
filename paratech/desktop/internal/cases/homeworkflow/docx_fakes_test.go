package homeworkflow

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// fakeProfiles is the ProfileSource: the stored profile and a read error.
type fakeProfiles struct {
	mu      sync.Mutex
	profile settings.ProfileFields
	err     error
}

func (f *fakeProfiles) Load(context.Context) (*settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	st := settings.New()
	p, err := settings.NewProfile(f.profile)
	if err != nil {
		return nil, err
	}
	if err := st.SetProfile(p, t0); err != nil {
		return nil, err
	}
	return st, nil
}

// fakeRenderer is the DocumentRenderer: it keeps the documents and renders each as its lines, so the bytes change
// with the document.
type fakeRenderer struct {
	mu   sync.Mutex
	docs []values.Document
	err  error
}

func (f *fakeRenderer) Render(_ context.Context, doc values.Document) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.docs = append(f.docs, doc)
	var b strings.Builder
	fmt.Fprintf(&b, "DOCX\n%+v\n", doc.Title)
	for _, bl := range doc.Blocks {
		fmt.Fprintf(&b, "%s %d %s\n", bl.Kind, bl.Level, strings.Join(bl.Lines, "|"))
	}
	return []byte(b.String()), nil
}

func (f *fakeRenderer) last() values.Document {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.docs) == 0 {
		return values.Document{}
	}
	return f.docs[len(f.docs)-1]
}

// memFiles is the DocumentWriter: the files written by path and a write error.
type memFiles struct {
	mu    sync.Mutex
	files map[string][]byte
	err   error
}

func (f *memFiles) WriteFile(_ context.Context, path string, content []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[path] = append([]byte(nil), content...)
	return nil
}

var (
	_ ProfileSource    = (*fakeProfiles)(nil)
	_ DocumentRenderer = (*fakeRenderer)(nil)
	_ DocumentWriter   = (*memFiles)(nil)
)
