package transcript

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func ref(t *testing.T, s string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.ParseArtifactRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func validSpec(t *testing.T) Spec {
	t.Helper()
	return Spec{
		ID: "job-1", RecordingID: "rec-1", ModelID: "m-1", ModelName: "base", ModelVersion: "1",
		Language: "ru", Text: ref(t, "transcripts/job-1.json"),
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Spec)
		at      time.Time
		wantErr bool
	}{
		{name: "valid", change: func(*Spec) {}, at: t0},
		{name: "no language", change: func(s *Spec) { s.Language = "" }, at: t0},
		{name: "three letter language", change: func(s *Spec) { s.Language = "yue" }, at: t0},
		{name: "no creation time", change: func(*Spec) {}, wantErr: true},
		{name: "empty id", change: func(s *Spec) { s.ID = "" }, at: t0, wantErr: true},
		{name: "bad id", change: func(s *Spec) { s.ID = "a/b" }, at: t0, wantErr: true},
		{name: "empty recording", change: func(s *Spec) { s.RecordingID = "" }, at: t0, wantErr: true},
		{name: "empty model id", change: func(s *Spec) { s.ModelID = "" }, at: t0, wantErr: true},
		{name: "empty model name", change: func(s *Spec) { s.ModelName = "" }, at: t0, wantErr: true},
		{name: "bad model name", change: func(s *Spec) { s.ModelName = "a b" }, at: t0, wantErr: true},
		{name: "empty model version", change: func(s *Spec) { s.ModelVersion = "" }, at: t0, wantErr: true},
		{name: "upper case language", change: func(s *Spec) { s.Language = "RU" }, at: t0, wantErr: true},
		{name: "one letter language", change: func(s *Spec) { s.Language = "r" }, at: t0, wantErr: true},
		{name: "no document", change: func(s *Spec) { s.Text = shared.ArtifactRef{} }, at: t0, wantErr: true},
		{name: "document of another kind", change: func(s *Spec) { s.Text = ref(t, "recordings/job-1.json") }, at: t0, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec(t)
			tc.change(&s)
			tr, err := New(s, tc.at)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidTranscript) {
					t.Fatalf("New error = %v, want ErrInvalidTranscript", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if tr.Version() != 0 {
				t.Errorf("Version = %d, want 0", tr.Version())
			}
			if tr.ID() != s.ID || tr.RecordingID() != s.RecordingID || tr.ModelID() != s.ModelID ||
				tr.ModelName() != s.ModelName || tr.ModelVersion() != s.ModelVersion || tr.Language() != s.Language ||
				tr.Text() != s.Text || !tr.CreatedAt().Equal(tc.at) {
				t.Errorf("accessors do not return the spec: %+v", tr)
			}
		})
	}
}

func TestRestore(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*State)
		wantErr bool
	}{
		{name: "valid", change: func(*State) {}},
		{name: "model removed keeps name and version", change: func(s *State) { s.ModelID = "" }},
		{name: "version zero", change: func(s *State) { s.Version = 0 }, wantErr: true},
		{name: "bad model id", change: func(s *State) { s.ModelID = "a b" }, wantErr: true},
		{name: "no model name", change: func(s *State) { s.ModelName = "" }, wantErr: true},
		{name: "no creation time", change: func(s *State) { s.CreatedAt = time.Time{} }, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := State{Spec: validSpec(t), Version: 3, CreatedAt: t0}
			tc.change(&s)
			tr, err := Restore(s)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidTranscript) {
					t.Fatalf("Restore error = %v, want ErrInvalidTranscript", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if tr.Version() != 3 || tr.ModelID() != s.ModelID || tr.ModelName() != "base" || tr.ModelVersion() != "1" {
				t.Errorf("restored %+v", tr)
			}
		})
	}
}

func TestPersisted(t *testing.T) {
	tr, err := New(validSpec(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	tr.Persisted(1)
	if tr.Version() != 1 {
		t.Fatalf("Version = %d, want 1", tr.Version())
	}
}

func TestDocumentRef(t *testing.T) {
	tests := []struct {
		id      shared.TranscriptID
		want    string
		wantErr bool
	}{
		{id: "job-1", want: "transcripts/job-1.json"},
		{id: "0b9d6f3e-4c1a-4d6e-9b53-6f1c2a7e8d10", want: "transcripts/0b9d6f3e-4c1a-4d6e-9b53-6f1c2a7e8d10.json"},
		{id: "", wantErr: true},
		{id: "../x", wantErr: true},
		{id: "nul", wantErr: true},
		{id: shared.TranscriptID(strings.Repeat("x", 129)), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			got, err := DocumentRef(tc.id)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DocumentRef(%q) = %s, want an error", tc.id, got)
				}
				return
			}
			if err != nil || got.String() != tc.want {
				t.Fatalf("DocumentRef(%q) = %s, %v; want %s", tc.id, got, err, tc.want)
			}
		})
	}
}

func TestCreatedEvent(t *testing.T) {
	tr, err := New(validSpec(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	want := Created{Transcript: "job-1", Recording: "rec-1", ModelName: values.ModelName("base"), ModelVersion: "1", Language: "ru"}
	if got := tr.Created(); got != want {
		t.Fatalf("Created = %+v, want %+v", got, want)
	}
}

func TestLanguageValidate(t *testing.T) {
	for _, tc := range []struct {
		lang Language
		ok   bool
	}{
		{"", true}, {"en", true}, {"haw", true}, {"abcdefgh", true},
		{"e", false}, {"abcdefghi", false}, {"En", false}, {"e1", false}, {"e-n", false}, {"рус", false},
	} {
		if err := tc.lang.Validate(); (err == nil) != tc.ok {
			t.Errorf("Validate(%q) = %v, want ok=%v", tc.lang, err, tc.ok)
		} else if err != nil && !errors.Is(err, ErrInvalidLanguage) {
			t.Errorf("Validate(%q) = %v, want ErrInvalidLanguage", tc.lang, err)
		}
	}
}
