package wails

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/subjectview"
)

// leaks are the parts of a storage error that must never reach the log: row data and a path.
var leaks = []string{"Петров", "petrov@example.invalid", "/home/x/secret.pdf", "boom"}

func leakingCause() error {
	return fmt.Errorf("db: row Петров petrov@example.invalid /home/x/secret.pdf: %w", errors.New("boom"))
}

func assertNoLeak(t *testing.T, log string) {
	t.Helper()
	for _, l := range leaks {
		if strings.Contains(log, l) {
			t.Errorf("the log has %q: %s", l, log)
		}
	}
}

// An unexpected use case error is logged by its class only: the chain may carry names, teachers and paths.
func TestLibraryFailLogsNoCause(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		cause string
	}{
		{"storage", fmt.Errorf("%w: %w", library.ErrUnavailable, leakingCause()), `cause="local storage"`},
		{"unexpected", leakingCause(), "cause=unexpected"},
		// A known sentinel logs its own text, not the chain that wraps it.
		{"known", fmt.Errorf("db: row Петров petrov@example.invalid /home/x/secret.pdf boom: %w", library.ErrSubjectNotFound),
			`result="` + library.ErrSubjectNotFound.Error() + `"`},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, logs := newLibraryBinding(t, "", &memFiles{})
			uc.err = tt.err

			_, err := s.Subjects(ctx)
			if tt.name == "known" {
				if !errors.Is(err, ErrLibrarySubjectNotFound) {
					t.Fatalf("Subjects = %v, want ErrLibrarySubjectNotFound", err)
				}
			} else if !errors.Is(err, ErrLibraryUnavailable) || err.Error() != ErrLibraryUnavailable.Error() {
				t.Fatalf("Subjects = %v, want ErrLibraryUnavailable", err)
			}

			out := logs.String()
			assertNoLeak(t, out)
			if !strings.Contains(out, tt.cause) {
				t.Errorf("the log lacks %s: %s", tt.cause, out)
			}
		})
	}
}

func TestLibrarySubjectProblemLogsNoCause(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.err = fmt.Errorf("%w: %w", library.ErrUnavailable, leakingCause())

	if got := s.DiscardSubject(context.Background(), "s1", 3); got.Problem != subjectFailed {
		t.Fatalf("DiscardSubject = %+v, want %q", got, subjectFailed)
	}
	out := logs.String()
	assertNoLeak(t, out)
	if !strings.Contains(out, `cause="local storage"`) {
		t.Errorf("the log lacks the cause: %s", out)
	}
}

func TestSubjectOverviewFailLogsNoCause(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		want  error
		cause string
	}{
		{"storage", fmt.Errorf("%w: %w", subjectview.ErrUnavailable, leakingCause()), ErrLibraryUnavailable, `cause="local storage"`},
		{"unexpected", leakingCause(), ErrLibraryUnavailable, "cause=unexpected"},
		{"known", fmt.Errorf("%w: %w", subjectview.ErrSubjectNotFound, leakingCause()), ErrLibrarySubjectNotFound,
			`result="` + subjectview.ErrSubjectNotFound.Error() + `"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			s := NewLibraryService(&fakeLibrary{}, nil, nil, slog.New(slog.NewTextHandler(logs, nil))).
				withOverview(&fakeOverview{err: tt.err, openErr: tt.err})

			if _, err := s.SubjectOverview(context.Background(), "s1"); !errors.Is(err, tt.want) {
				t.Errorf("SubjectOverview = %v, want %v", err, tt.want)
			}
			if err := s.MarkSubjectOpened(context.Background(), "s1"); !errors.Is(err, tt.want) {
				t.Errorf("MarkSubjectOpened = %v, want %v", err, tt.want)
			}

			out := logs.String()
			assertNoLeak(t, out)
			if !strings.Contains(out, tt.cause) {
				t.Errorf("the log lacks %s: %s", tt.cause, out)
			}
		})
	}
}
