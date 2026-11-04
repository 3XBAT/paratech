package wails

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// fakeHooks answer every dialog with "<dialog>:<argument>" and record the calls.
type fakeHooks struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeHooks) note(s string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	return s
}

func (f *fakeHooks) PickFile(_ context.Context, dialog string) (string, error) {
	return f.note("open:" + dialog), nil
}

func (f *fakeHooks) PickFiles(_ context.Context, dialog string, multiple bool) ([]string, error) {
	return []string{f.note(fmt.Sprintf("open:%s:%v", dialog, multiple))}, nil
}

func (f *fakeHooks) SaveFile(_ context.Context, dialog, name string) (string, error) {
	return f.note("save:" + dialog + ":" + name), nil
}

func (f *fakeHooks) SetText(_ context.Context, text string) error {
	f.note("clipboard:" + text)
	return nil
}

func (f *fakeHooks) OpenLink(_ context.Context, u values.SourceURL) error {
	f.note("link:" + u.String())
	return nil
}

func (f *fakeHooks) OpenArtifact(_ context.Context, ref shared.ArtifactRef) error {
	f.note("file:" + ref.String())
	return nil
}

func (f *fakeHooks) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// dialogCall calls one dialog of d and returns what it answered.
type dialogCall struct {
	name string // the native forwarder it stands for
	call func(d dialogs) (string, error)
}

func dialogCalls() []dialogCall {
	ctx := context.Background()
	one := func(r string, err error) (string, error) { return r, err }
	many := func(r []string, err error) (string, error) {
		if len(r) == 0 {
			return "", err
		}
		return r[0], err
	}
	return []dialogCall{
		{"pickScheduleFile", func(d dialogs) (string, error) { return one(d.schedule(ctx)) }},
		{"pickModelFile", func(d dialogs) (string, error) { return one(d.model(ctx)) }},
		{"pickMaterialFile", func(d dialogs) (string, error) { return one(d.material(ctx)) }},
		{"pickRecordingFile", func(d dialogs) (string, error) { return one(d.recording(ctx)) }},
		{"pickNoteFiles", func(d dialogs) (string, error) { return many(d.notes(ctx, true)) }},
		{"pickBundleFile", func(d dialogs) (string, error) { return one(d.bundle(ctx, "b.zip")) }},
		{"pickDocxFile", func(d dialogs) (string, error) { return one(d.docx(ctx, "h.docx")) }},
		{"pickKnowledgeFile", func(d dialogs) (string, error) { return one(d.knowledge(ctx, "Физика.md")) }},
		{"pickTranscriptFile", func(d dialogs) (string, error) { return one(d.transcript(ctx, "t.txt", "txt")) }},
		{"pickSummaryFile", func(d dialogs) (string, error) { return one(d.summary(ctx, "s.md", "md")) }},
		{"setClipboardText", func(d dialogs) (string, error) { return "", d.clipboard(ctx, "text") }},
	}
}

// Without hooks every dialog is the native one of the running shell, as before.
func TestDialogsNative(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	d := s.dialogs()
	for _, c := range dialogCalls() {
		if _, err := c.call(d); !errors.Is(err, ErrNotRunning) {
			t.Errorf("%s before Run: %v, want ErrNotRunning (the native dialog)", c.name, err)
		}
	}

	f := &fakeUI{}
	s.setUI(f)
	var want []string
	for _, c := range dialogCalls() {
		_, err := c.call(d)
		if c.name == "pickRecordingFile" {
			// fakeUI has no recording dialog: the native forwarder says so.
			if !errors.Is(err, ErrUnsupported) {
				t.Errorf("pickRecordingFile: %v, want ErrUnsupported", err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		want = append(want, c.name)
	}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("native calls %q, want %q", f.calls, want)
	}
}

// With hooks every dialog and the clipboard go to them with the fixed dialog name, never to the native shell; the
// shell need not even run.
func TestDialogsHooked(t *testing.T) {
	h := &fakeHooks{}
	opts := validOptions()
	opts.Hooks = Hooks{Open: h, Save: h, Clipboard: h}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeUI{}
	s.setUI(f)

	want := map[string]string{
		"pickScheduleFile":   "open:schedule",
		"pickModelFile":      "open:model",
		"pickMaterialFile":   "open:material",
		"pickRecordingFile":  "open:recording",
		"pickNoteFiles":      "open:notes:true",
		"pickBundleFile":     "save:bundle:b.zip",
		"pickDocxFile":       "save:docx:h.docx",
		"pickKnowledgeFile":  "save:knowledge:Физика.md",
		"pickTranscriptFile": "save:transcript:t.txt",
		"pickSummaryFile":    "save:summary:s.md",
	}
	d := s.dialogs()
	for _, c := range dialogCalls() {
		got, err := c.call(d)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != want[c.name] {
			t.Errorf("%s answered %q, want %q", c.name, got, want[c.name])
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("native dialogs shown with hooks: %q", f.calls)
	}
	if calls := h.recorded(); len(calls) == 0 || calls[len(calls)-1] != "clipboard:text" {
		t.Fatalf("clipboard not hooked: %q", calls)
	}
	if got, _ := d.notes(context.Background(), false); !slices.Equal(got, []string{"open:notes:false"}) {
		t.Fatalf("single notes file: %q", got)
	}
}

// No bound method takes or returns the hooks, also not inside a struct, slice or map: the page cannot reach them.
func TestBoundServicesDoNotExposeHooks(t *testing.T) {
	hidden := map[reflect.Type]bool{
		reflect.TypeFor[Hooks](): true, reflect.TypeFor[OpenDialogs](): true, reflect.TypeFor[SaveDialogs](): true,
		reflect.TypeFor[Options](): true,
	}
	var exposes func(t reflect.Type, seen map[reflect.Type]bool) bool
	exposes = func(t reflect.Type, seen map[reflect.Type]bool) bool {
		if hidden[t] {
			return true
		}
		if seen[t] {
			return false
		}
		seen[t] = true
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			return exposes(t.Elem(), seen)
		case reflect.Map:
			return exposes(t.Key(), seen) || exposes(t.Elem(), seen)
		case reflect.Struct:
			for i := range t.NumField() {
				if exposes(t.Field(i).Type, seen) {
					return true
				}
			}
		}
		return false
	}

	if !exposes(reflect.TypeFor[[]struct{ O *Options }](), map[reflect.Type]bool{}) {
		t.Fatal("the check misses a nested hook")
	}

	services := reflect.TypeFor[Services]()
	for i := range services.NumField() {
		field := services.Field(i)
		if field.Name == "AgentPanel" { // not bound
			continue
		}
		for j := range field.Type.NumMethod() {
			m := field.Type.Method(j)
			for k := 1; k < m.Type.NumIn(); k++ { // 0 is the receiver
				if exposes(m.Type.In(k), map[reflect.Type]bool{}) {
					t.Errorf("%s.%s takes the hooks", field.Name, m.Name)
				}
			}
			for k := range m.Type.NumOut() {
				if exposes(m.Type.Out(k), map[reflect.Type]bool{}) {
					t.Errorf("%s.%s returns the hooks", field.Name, m.Name)
				}
			}
		}
	}
}

// The hooks reach the services OpenServices builds: links, library files, dialogs and the clipboard.
func TestOpenServicesUsesHooks(t *testing.T) {
	h := &fakeHooks{}
	opts := validOptions()
	opts.Hooks = Hooks{Open: h, Save: h, Clipboard: h, Links: h, Opener: h}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.OpenServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Help.links != LinkOpener(h) || b.Models.deps.Links != LinkOpener(h) {
		t.Fatal("the link opener of the help and models pages is not the hook")
	}
	if b.Library.opener != FileOpener(h) {
		t.Fatal("the file opener of the library is not the hook")
	}
	if err := b.Help.OpenHelpPage(context.Background(), "google-syncselect"); err != nil {
		t.Fatal(err)
	}
	if calls := h.recorded(); !slices.Equal(calls, []string{"link:https://www.google.com/calendar/syncselect"}) {
		t.Fatalf("hook calls %q", calls)
	}

	// Without hooks the core's own openers stay.
	plain, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err = plain.OpenServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Help.links.(*fakeLinks); !ok {
		t.Fatalf("help links %T, want the core's", b.Help.links)
	}
	if _, ok := b.Library.opener.(*fakeOpener); !ok {
		t.Fatalf("library opener %T, want the core's", b.Library.opener)
	}
}
