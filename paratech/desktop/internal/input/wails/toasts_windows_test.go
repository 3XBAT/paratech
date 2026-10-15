//go:build windows

package wails

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// These tests use a fake of the Wails notifications service: the real startup writes the registry and the real send
// shows a toast, neither may happen in a test.

type fakeWailsNotifier struct {
	startupErr   error
	startupPanic bool
	sendErr      error

	started, shutdowns int
	sent               []notifications.NotificationOptions
}

func (f *fakeWailsNotifier) ServiceStartup(context.Context, application.ServiceOptions) error {
	f.started++
	if f.startupPanic {
		panic("registry exploded")
	}
	return f.startupErr
}

func (f *fakeWailsNotifier) ServiceShutdown() error { f.shutdowns++; return nil }

func (f *fakeWailsNotifier) SendNotification(o notifications.NotificationOptions) error {
	f.sent = append(f.sent, o)
	return f.sendErr
}

func TestToastServiceStartsAndPushesPlainToasts(t *testing.T) {
	f := &fakeWailsNotifier{}
	ts := newToastService(f, discard())
	tt := toast{id: "job-1-completed", title: "Транскрипт готов", body: "Запись урока расшифрована."}

	if err := ts.push(tt); !errors.Is(err, ErrNotificationsUnavailable) || len(f.sent) != 0 {
		t.Fatalf("push before startup: err = %v, sent %d", err, len(f.sent))
	}
	if err := ts.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil || f.started != 1 {
		t.Fatalf("startup: err = %v, started %d", err, f.started)
	}
	if err := ts.push(tt); err != nil {
		t.Fatal(err)
	}
	want := notifications.NotificationOptions{ID: tt.id, Title: tt.title, Body: tt.body}
	if len(f.sent) != 1 || !reflect.DeepEqual(f.sent[0], want) {
		t.Fatalf("sent %+v, want only %+v: no scenario, sound, schedule or data", f.sent, want)
	}

	boom := errors.New("COM failed")
	f.sendErr = boom
	if err := ts.push(tt); !errors.Is(err, boom) {
		t.Fatalf("failing send: err = %v", err)
	}

	if err := ts.ServiceShutdown(); err != nil || f.shutdowns != 1 {
		t.Fatalf("shutdown: err = %v, shutdowns %d", err, f.shutdowns)
	}
	if err := ts.push(tt); !errors.Is(err, ErrNotificationsUnavailable) {
		t.Fatalf("push after shutdown: err = %v", err)
	}
	if err := ts.ServiceShutdown(); err != nil || f.shutdowns != 1 {
		t.Fatalf("second shutdown: err = %v, shutdowns %d", err, f.shutdowns)
	}
}

// A failing startup must not abort App.Run (Wails stops the app when a service startup returns an error).
func TestToastServiceStartupFailureKeepsTheAppRunning(t *testing.T) {
	tests := []struct {
		name string
		fake *fakeWailsNotifier
		log  string
	}{
		{"error", &fakeWailsNotifier{startupErr: errors.New("access denied to HKCU")}, "access denied"},
		{"panic", &fakeWailsNotifier{startupPanic: true}, "registry exploded"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			ts := newToastService(tc.fake, slog.New(slog.NewTextHandler(&logs, nil)))
			if err := ts.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
				t.Fatalf("startup returned %v, want nil", err)
			}
			if !strings.Contains(logs.String(), tc.log) {
				t.Errorf("log %q does not name the cause", logs.String())
			}
			if err := ts.push(toast{id: "x", title: "y"}); !errors.Is(err, ErrNotificationsUnavailable) || len(tc.fake.sent) != 0 {
				t.Fatalf("push after a failed startup: err = %v, sent %d", err, len(tc.fake.sent))
			}
			if err := ts.ServiceShutdown(); err != nil || tc.fake.shutdowns != 0 {
				t.Fatalf("shutdown of a service that never started: err = %v, shutdowns %d", err, tc.fake.shutdowns)
			}
		})
	}
}

// Wails binds every exported method of a service to the pages except its lifecycle hooks; the toast service must
// expose nothing.
func TestToastServiceBindsNothing(t *testing.T) {
	hooks := map[string]bool{"ServiceName": true, "ServiceStartup": true, "ServiceShutdown": true}
	typ := reflect.TypeFor[*toastService]()
	for i := range typ.NumMethod() {
		if name := typ.Method(i).Name; !hooks[name] {
			t.Errorf("toastService exports %s, which the pages could call", name)
		}
	}
	if (&toastService{}).ServiceName() == "" {
		t.Error("empty service name")
	}
}

func TestWindowsUINotifyUsesTheToastService(t *testing.T) {
	f := &fakeWailsNotifier{}
	ts := newToastService(f, discard())
	if err := ts.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	u := &windowsUI{toasts: ts}
	tt := toast{id: "job-1-failed", title: "Обработка не удалась", body: "Подробности в приложении."}
	if err := u.notify(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.sent[0].ID != tt.id {
		t.Fatalf("sent %+v", f.sent)
	}
}
