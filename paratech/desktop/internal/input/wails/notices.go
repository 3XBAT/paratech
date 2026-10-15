package wails

import (
	"context"
	"errors"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

var (
	// ErrNotificationsUnavailable means Windows notifications could not be set up at start (see toasts_windows.go); the
	// app runs without them.
	ErrNotificationsUnavailable = errors.New("wails: notifications are unavailable")
	// ErrNoNotice means Notify was called without a notice.
	ErrNoNotice = errors.New("wails: no notice to show")
)

// toast is one notification as the shell shows it: fixed texts only, never user content such as lesson titles, error
// messages or model output, because toasts may show on a locked screen and model output is untrusted. Also, the
// PowerShell fallback of the toast library pastes the toast XML unescaped into a script (a "@ here-string where $
// expands), so the texts must stay fixed and free of PowerShell syntax; TestNoticeTexts checks it.
type toast struct {
	id    string
	title string
	body  string
}

// toastFor builds the toast of a notice. The id is stable per job and status.
func toastFor(n *notice.Notice) toast {
	title, body := noticeText(n.Type(), n.Status())
	return toast{id: "job-" + string(n.Job()) + "-" + strings.ToLower(string(n.Status())), title: title, body: body}
}

// noticeText returns the fixed Russian texts for a job type and a finished status, with a generic fallback.
func noticeText(typ job.Type, status job.Status) (title, body string) {
	failed := status == job.StatusFailed
	switch string(typ) {
	case transcription.STTJobType:
		if failed {
			return "Не удалось расшифровать запись", "Подробности в приложении."
		}
		return "Транскрипт готов", "Запись урока расшифрована."
	case values.SummarizeJobType:
		if failed {
			return "Не удалось составить конспект", "Подробности в приложении."
		}
		return "Конспект готов", "Краткое содержание урока готово."
	case values.ExtractHomeworkJobType:
		if failed {
			return "Не удалось найти домашние задания", "Подробности в приложении."
		}
		return "Поиск заданий завершён", "Проверьте найденные домашние задания."
	case values.HomeworkDraftJobType:
		if failed {
			return "Не удалось подготовить черновик", "Подробности в приложении."
		}
		return "Черновик готов", "Черновик домашнего задания готов к проверке."
	}
	if failed {
		return "Обработка не удалась", "Подробности в приложении."
	}
	return "Обработка завершена", "Результат можно посмотреть в приложении."
}

// pusher shows a toast; it may block, for example in Windows' PowerShell fallback.
type pusher interface {
	push(t toast) error
}

// pushWithin shows t but returns when ctx ends; a push that is still running then finishes in the background.
func pushWithin(ctx context.Context, p pusher, t toast) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- p.push(t) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
