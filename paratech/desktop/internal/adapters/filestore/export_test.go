package filestore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
)

// exportCommand is the command of a confirmed export_homework proposal of assignment a1 with the given body.
func exportCommand(t *testing.T, body string) proposal.ExportCommand {
	t.Helper()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	out := values.NewUntrustedOutput(`{"kind":"export_homework","assignment_id":"a1","body":"` + body + `"}`)
	p, err := proposal.New("p1", out, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true, AssignmentTitle: "Rivers"}, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm(now); err != nil {
		t.Fatal(err)
	}
	cmd, err := p.ExportCommand()
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestExportHomeworkWritesTheExportFile(t *testing.T) {
	ctx := context.Background()
	s, root := newStore(t)
	file := filepath.Join(root, "exports", "a1", "homework.md")

	cmd := exportCommand(t, "Deltas.")
	want := values.HomeworkMarkdown("Rivers", "Deltas.")
	for i := range 2 { // the same command again yields the same file
		if err := s.ExportHomework(ctx, cmd); err != nil {
			t.Fatalf("ExportHomework #%d: %v", i+1, err)
		}
		data, err := os.ReadFile(file)
		if err != nil || string(data) != want {
			t.Fatalf("export file after #%d = %q, %v; want %q", i+1, data, err, want)
		}
	}

	// A later proposal replaces the file atomically.
	if err := s.ExportHomework(ctx, exportCommand(t, "Estuaries.")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != values.HomeworkMarkdown("Rivers", "Estuaries.") {
		t.Fatalf("export file = %q, %v", data, err)
	}
}

func TestExportHomeworkRefusesWithoutWriting(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		cmd  proposal.ExportCommand
		want error
	}{
		{"zero command", context.Background(), proposal.ExportCommand{}, ErrZeroCommand},
		{"cancelled context", cancelled, exportCommand(t, "Deltas."), context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newStore(t)
			if err := s.ExportHomework(tc.ctx, tc.cmd); !errors.Is(err, tc.want) {
				t.Fatalf("ExportHomework = %v, want %v", err, tc.want)
			}
			entries, err := os.ReadDir(filepath.Join(root, "exports"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("exports/ after a refused export: %v, %v; want empty", entries, err)
			}
			if _, err := os.Stat(filepath.Join(root, "exports", "a1")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("exports/a1 exists: %v", err)
			}
		})
	}
}
