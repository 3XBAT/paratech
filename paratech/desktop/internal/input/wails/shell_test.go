package wails

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
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
		Settings: &fakeSettings{}, Activity: &fakeActivity{}, Stopper: &fakeStopper{}, Forcer: &fakeForcer{}, Retrier: &fakeRetrier{},
		Prompt: &fakePrompt{}, Schedule: &fakeSchedule{}, Files: disk(),
		Models: &fakeCatalog{}, Downloads: &fakeDownloads{}, Imports: &fakeImports{}, Links: &fakeLinks{},
		Speech:      &fakeSpeech{},
		Connections: &fakeConnections{}, Google: &fakeGoogle{}, Calendar: &fakeCalendarSync{},
		Sound: &fakeSound{}, Bundle: &fakeBundle{},
		Homework:        validHomework(),
		Transcript:      &fakeTranscript{},
		Summary:         &fakeSummary{},
		Deletion:        &fakeDeletion{},
		RecordingImport: &fakeImportUC{},
		Knowledge:       &fakeKnowledge{},
		Library:         &fakeLibrary{}, MaterialViews: newMemViews(), Opener: &fakeOpener{}, SubjectOverview: &fakeOverview{},
		Notes: &fakeNotesUC{},
		Agent: AgentCore{
			Driver: &fakeAgentDriver{}, Link: &fakeConnections{}, Panel: NewAgentPanel(discard()), Life: context.Background(),
			Journal: &fakeJournal{}, Clock: fixedClock{}, Chats: &fakeChats{},
		},
		Today:        &fakeToday{},
		AgentConfirm: AgentConfirmCore{Proposals: newConfirmProposals(), Materials: fakeMaterials{}, Deletions: &fakeDeletion{}},
		Turnkey:      &fakeTurnkey{},
		Answers:      &fakeAnswers{},
		Tickets:      &fakeTickets{},
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
type fakeUI struct{ calls []string }

func (f *fakeUI) showPopup() error { f.calls = append(f.calls, "showPopup"); return nil }
func (f *fakeUI) hidePopup() error { f.calls = append(f.calls, "hidePopup"); return nil }
func (f *fakeUI) showMain() error  { f.calls = append(f.calls, "showMain"); return nil }

func (f *fakeUI) fitPopup(height int) (int, error) {
	f.calls = append(f.calls, "fitPopup")
	return height, nil
}

func (f *fakeUI) pickScheduleFile() (string, error) {
	f.calls = append(f.calls, "pickScheduleFile")
	return "", nil
}

func (f *fakeUI) pickModelFile() (string, error) {
	f.calls = append(f.calls, "pickModelFile")
	return "", nil
}

func (f *fakeUI) pickBundleFile(string) (string, error) {
	f.calls = append(f.calls, "pickBundleFile")
	return "", nil
}

func (f *fakeUI) pickTranscriptFile(string, string) (string, error) {
	f.calls = append(f.calls, "pickTranscriptFile")
	return "", nil
}

func (f *fakeUI) pickSummaryFile(string, string) (string, error) {
	f.calls = append(f.calls, "pickSummaryFile")
	return "", nil
}

func (f *fakeUI) pickDocxFile(string) (string, error) {
	f.calls = append(f.calls, "pickDocxFile")
	return "", nil
}

func (f *fakeUI) pickKnowledgeFile(string) (string, error) {
	f.calls = append(f.calls, "pickKnowledgeFile")
	return "", nil
}

func (f *fakeUI) pickMaterialFile() (string, error) {
	f.calls = append(f.calls, "pickMaterialFile")
	return "", nil
}

func (f *fakeUI) pickNoteFiles(bool) ([]string, error) {
	f.calls = append(f.calls, "pickNoteFiles")
	return nil, nil
}

func (f *fakeUI) setClipboardText(string) error {
	f.calls = append(f.calls, "setClipboardText")
	return nil
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
		{"fitPopup", func() error { _, err := s.fitPopup(360); return err }},
		{"pickScheduleFile", func() error { _, err := s.pickScheduleFile(context.Background()); return err }},
		{"pickModelFile", func() error { _, err := s.pickModelFile(context.Background()); return err }},
		{"pickBundleFile", func() error { _, err := s.pickBundleFile(context.Background(), "b.zip"); return err }},
		{"pickTranscriptFile", func() error { _, err := s.pickTranscriptFile(context.Background(), "t.txt", "txt"); return err }},
		{"pickSummaryFile", func() error { _, err := s.pickSummaryFile(context.Background(), "s.md", "md"); return err }},
		{"pickDocxFile", func() error { _, err := s.pickDocxFile(context.Background(), "h.docx"); return err }},
		{"pickKnowledgeFile", func() error { _, err := s.pickKnowledgeFile(context.Background(), "Физика.md"); return err }},
		{"pickMaterialFile", func() error { _, err := s.pickMaterialFile(context.Background()); return err }},
		{"pickNoteFiles", func() error { _, err := s.pickNoteFiles(context.Background(), true); return err }},
		{"setClipboardText", func() error { return s.setClipboardText(context.Background(), "text") }},
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
