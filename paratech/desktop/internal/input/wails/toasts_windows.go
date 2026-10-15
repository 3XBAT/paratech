//go:build windows

package wails

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// toastServiceName names the notification service in Wails logs.
const toastServiceName = "learning-assistant/notifications"

// wailsNotifier is the part of the Wails notifications service the shell uses. Tests replace it, so no test writes the
// registry or shows a toast.
type wailsNotifier interface {
	ServiceStartup(ctx context.Context, options application.ServiceOptions) error
	ServiceShutdown() error
	SendNotification(options notifications.NotificationOptions) error
}

// toastService wraps the Wails notifications service. Its startup writes HKCU registry keys (the AppUserModelId and
// the COM activator CLSID that toasts need), and Wails aborts App.Run when a service fails to start, so the wrapper logs
// such a failure and the app runs without notifications. Apart from the service hooks it has no exported methods, so
// nothing of it is bound to the pages.
type toastService struct {
	impl wailsNotifier
	log  *slog.Logger

	mu    sync.Mutex
	ready bool
}

func newToastService(impl wailsNotifier, log *slog.Logger) *toastService {
	return &toastService{impl: impl, log: log}
}

// ServiceName names the service for Wails.
func (t *toastService) ServiceName() string { return toastServiceName }

// ServiceStartup starts the Wails service. It always returns nil: a failure or a panic only leaves notifications off.
func (t *toastService) ServiceStartup(ctx context.Context, options application.ServiceOptions) (err error) {
	defer func() {
		if p := recover(); p != nil {
			t.log.Error("notifications: startup panicked, running without notifications", "panic", fmt.Sprint(p))
			err = nil
		}
	}()

	if err := t.impl.ServiceStartup(ctx, options); err != nil {
		t.log.Error("notifications: startup failed, running without notifications", "err", err)
		return nil
	}
	t.mu.Lock()
	t.ready = true
	t.mu.Unlock()
	return nil
}

// ServiceShutdown stops the Wails service if it started.
func (t *toastService) ServiceShutdown() error {
	t.mu.Lock()
	ready := t.ready
	t.ready = false
	t.mu.Unlock()

	if !ready {
		return nil
	}
	return t.impl.ServiceShutdown()
}

// isReady reports whether the service started and did not shut down.
func (t *toastService) isReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready
}

// push shows a plain toast: no scenario (the "urgent" and "reminder" ones can break through Focus Assist), default
// sound and duration, no schedule. Windows applies the user's notification settings to it.
func (t *toastService) push(tt toast) error {
	if !t.isReady() {
		return ErrNotificationsUnavailable
	}

	if err := t.impl.SendNotification(notifications.NotificationOptions{ID: tt.id, Title: tt.title, Body: tt.body}); err != nil {
		return fmt.Errorf("wails: show notification: %w", err)
	}
	return nil
}
