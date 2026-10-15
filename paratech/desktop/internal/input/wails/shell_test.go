package wails

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func validOptions() Options {
	return Options{
		Name: "LA", InstanceID: "test.la", Log: discard(),
		OpenCore: func(context.Context) (Core, error) { return validCore(), nil },
	}
}

// validCore has every part the bindings need.
func validCore() Core {
	return Core{
		Settings: &fakeSettings{}, Activity: &fakeActivity{}, Stopper: &fakeStopper{}, Forcer: &fakeForcer{},
		Prompt: &fakePrompt{}, Schedule: &fakeSchedule{}, Files: disk(),
		Models: &fakeCatalog{}, Downloads: &fakeDownloads{}, Imports: &fakeImports{}, Links: &fakeLinks{},
		Connections: &fakeConnections{},
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*Options)
		wantErr bool
	}{
		{name: "ok", edit: func(*Options) {}},
		{name: "no name", edit: func(o *Options) { o.Name = "" }, wantErr: true},
		{name: "no instance id", edit: func(o *Options) { o.InstanceID = "" }, wantErr: true},
		{name: "no logger", edit: func(o *Options) { o.Log = nil }, wantErr: true},
		{name: "no core opener", edit: func(o *Options) { o.OpenCore = nil }, wantErr: true},
		{name: "started hook", edit: func(o *Options) { o.Started = func() {} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := validOptions()
			tt.edit(&opts)
			s, err := New(opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && s == nil {
				t.Fatal("nil shell without error")
			}
		})
	}
}

// fakeUI records forwarded commands.
type fakeUI struct {
	calls    []string
	toasts   []toast
	noToasts bool // Windows notifications failed to set up
}

func (f *fakeUI) showPopup() error { f.calls = append(f.calls, "showPopup"); return nil }
func (f *fakeUI) hidePopup() error { f.calls = append(f.calls, "hidePopup"); return nil }
func (f *fakeUI) showMain() error  { f.calls = append(f.calls, "showMain"); return nil }

func (f *fakeUI) notify(_ context.Context, t toast) error {
	f.toasts = append(f.toasts, t)
	return nil
}

func (f *fakeUI) canNotify() bool { return !f.noToasts }

func (f *fakeUI) pickScheduleFile() (string, error) {
	f.calls = append(f.calls, "pickScheduleFile")
	return "", nil
}

func (f *fakeUI) pickModelFile() (string, error) {
	f.calls = append(f.calls, "pickModelFile")
	return "", nil
}

func TestCommands(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	commands := []struct {
		name string
		call func() error
	}{
		{"showPopup", s.ShowPopup},
		{"hidePopup", s.HidePopup},
		{"showMain", s.ShowMain},
		{"pickScheduleFile", func() error { _, err := s.pickScheduleFile(context.Background()); return err }},
		{"pickModelFile", func() error { _, err := s.pickModelFile(context.Background()); return err }},
	}

	for _, c := range commands {
		if err := c.call(); !errors.Is(err, ErrNotRunning) {
			t.Errorf("%s before Run: err = %v, want ErrNotRunning", c.name, err)
		}
	}

	f := &fakeUI{}
	s.setUI(f)
	for _, c := range commands {
		if err := c.call(); err != nil {
			t.Errorf("%s while running: %v", c.name, err)
		}
	}
	if len(f.calls) != len(commands) {
		t.Fatalf("forwarded calls = %v, want one per command", f.calls)
	}
	for i, c := range commands {
		if f.calls[i] != c.name {
			t.Errorf("call %d = %s, want %s", i, f.calls[i], c.name)
		}
	}
}

func TestMarkStartedOnce(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.markStarted(); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := s.markStarted(); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second start: err = %v, want ErrAlreadyStarted", err)
	}
}

func TestNotify(t *testing.T) {
	ctx := context.Background()
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	n, err := notice.New("job-7", "stt", job.StatusCompleted, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Notify(ctx, n); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Notify before Run: err = %v, want ErrNotRunning", err)
	}

	f := &fakeUI{}
	s.setUI(f)
	if err := s.Notify(ctx, nil); !errors.Is(err, ErrNoNotice) {
		t.Fatalf("Notify(nil): err = %v, want ErrNoNotice", err)
	}
	if err := s.Notify(ctx, n); err != nil {
		t.Fatal(err)
	}
	if len(f.toasts) != 1 || f.toasts[0] != toastFor(n) {
		t.Fatalf("toasts = %+v, want the toast of the notice", f.toasts)
	}

	s.setUI(nil) // stopped
	if err := s.Notify(ctx, n); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Notify after stop: err = %v, want ErrNotRunning", err)
	}
}

func TestReady(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if s.Ready() {
		t.Fatal("ready before Run")
	}

	f := &fakeUI{}
	s.setUI(f)
	if !s.Ready() {
		t.Fatal("not ready while running with notifications")
	}
	f.noToasts = true
	if s.Ready() {
		t.Fatal("ready although notifications could not be set up")
	}

	f.noToasts = false
	s.setUI(nil) // stopped
	if s.Ready() {
		t.Fatal("ready after stop")
	}
}
