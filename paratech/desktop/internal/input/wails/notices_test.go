package wails

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

var noticeT0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func TestNoticeTexts(t *testing.T) {
	known := []job.Type{
		transcription.STTJobType, values.SummarizeJobType, values.ExtractHomeworkJobType, values.HomeworkDraftJobType,
	}
	fallback := map[job.Status][2]string{}
	for _, s := range notice.Statuses() {
		title, body := noticeText("unknown_type", s)
		fallback[s] = [2]string{title, body}
		if title == "" || strings.ContainsAny(title+body, "$`\"'@<>&") {
			t.Errorf("fallback %s: %q %q is empty or has characters XML or PowerShell would interpret", s, title, body)
		}
	}
	if fallback[job.StatusCompleted] == fallback[job.StatusFailed] {
		t.Fatal("the fallback does not tell success from failure")
	}

	seen := map[string]job.Type{}
	for _, typ := range known {
		for _, s := range notice.Statuses() {
			title, body := noticeText(typ, s)
			if title == "" || body == "" {
				t.Errorf("%s %s: empty text", typ, s)
			}
			if strings.ContainsAny(title+body, "$`\"'@<>&") {
				t.Errorf("%s %s: %q %q has characters that XML or the PowerShell fallback would interpret", typ, s, title, body)
			}
			if !strings.ContainsFunc(title, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) {
				t.Errorf("%s %s: title %q is not Russian", typ, s, title)
			}
			if [2]string{title, body} == fallback[s] {
				t.Errorf("%s %s: uses the generic fallback, want its own text", typ, s)
			}
			if other, dup := seen[title]; dup {
				t.Errorf("%s %s: title %q is also used by %s", typ, s, title, other)
			}
			seen[title] = typ
		}
	}
}

func TestToastFor(t *testing.T) {
	n, err := notice.New("job-42", "stt", job.StatusFailed, noticeT0)
	if err != nil {
		t.Fatal(err)
	}
	got := toastFor(n)
	title, body := noticeText("stt", job.StatusFailed)
	if got.id != "job-job-42-failed" || got.title != title || got.body != body {
		t.Fatalf("toast = %+v", got)
	}

	// Stable per job and status: a repeat replaces nothing new, another status is another toast.
	again, _ := notice.New("job-42", "stt", job.StatusFailed, noticeT0.Add(time.Hour))
	done, _ := notice.New("job-42", "stt", job.StatusCompleted, noticeT0)
	if toastFor(again).id != got.id || toastFor(done).id == got.id {
		t.Fatalf("ids: %q %q %q", got.id, toastFor(again).id, toastFor(done).id)
	}
}

// pushFunc adapts a function to pusher.
type pushFunc func(toast) error

func (f pushFunc) push(t toast) error { return f(t) }

func TestPushWithin(t *testing.T) {
	tt := toast{id: "job-1-completed", title: "Транскрипт готов", body: "x"}

	t.Run("returns the push result", func(t *testing.T) {
		boom := errors.New("toast failed")
		var got toast
		err := pushWithin(context.Background(), pushFunc(func(t toast) error { got = t; return boom }), tt)
		if !errors.Is(err, boom) || got != tt {
			t.Fatalf("err = %v, pushed %+v", err, got)
		}
	})

	t.Run("returns when ctx ends", func(t *testing.T) {
		release := make(chan struct{})
		finished := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		err := pushWithin(ctx, pushFunc(func(toast) error {
			defer close(finished)
			<-release
			return nil
		}), tt)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		close(release)
		<-finished // the abandoned push ends on its own
	})

	t.Run("does not push with an ended ctx", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := pushWithin(ctx, pushFunc(func(toast) error { t.Error("pushed"); return nil }), tt)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}
