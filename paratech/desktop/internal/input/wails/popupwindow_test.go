package wails

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestPopupWindowServiceMethods(t *testing.T) {
	typ := reflect.TypeFor[*PopupWindowService]()
	if typ.NumMethod() != 1 || typ.Method(0).Name != "Fit" {
		t.Fatalf("PopupWindowService has %d exported methods, want only Fit", typ.NumMethod())
	}
}

func TestPopupWindowServiceFit(t *testing.T) {
	var asked []int
	ok := popupFitterFunc(func(h int) (int, error) {
		asked = append(asked, h)
		return min(h, popupMaxHeight), nil
	})
	s := NewPopupWindowService(ok, discard())
	got, err := s.Fit(500)
	if err != nil || got != popupMaxHeight {
		t.Fatalf("Fit(500) = %d, %v; want %d", got, err, popupMaxHeight)
	}
	if len(asked) != 1 || asked[0] != 500 {
		t.Fatalf("fitter asked %v, want [500]", asked)
	}

	var logs bytes.Buffer
	failing := popupFitterFunc(func(int) (int, error) { return 0, ErrNotRunning })
	s = NewPopupWindowService(failing, slog.New(slog.NewTextHandler(&logs, nil)))
	if _, err := s.Fit(320); !errors.Is(err, ErrActionFailed) {
		t.Fatalf("Fit with a stopped shell: err = %v, want ErrActionFailed", err)
	}
	if !strings.Contains(logs.String(), "not running") {
		t.Errorf("the cause must be logged, got %q", logs.String())
	}
}

func TestOpenServicesBindsPopupToShell(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.OpenServices(t.Context())
	if err != nil || b.Popup == nil {
		t.Fatalf("OpenServices = %+v, %v", b, err)
	}
	if _, err := b.Popup.Fit(350); !errors.Is(err, ErrActionFailed) {
		t.Fatalf("Fit before Run: err = %v, want ErrActionFailed", err)
	}

	f := &fakeUI{}
	s.setUI(f)
	if got, err := b.Popup.Fit(350); err != nil || got != 350 {
		t.Fatalf("Fit while running = %d, %v", got, err)
	}
	if len(f.calls) != 1 || f.calls[0] != "fitPopup" {
		t.Fatalf("forwarded calls = %v, want [fitPopup]", f.calls)
	}
}
