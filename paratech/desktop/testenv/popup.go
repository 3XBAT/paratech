package testenv

import "sync"

// FakePopup stands for the recording popup of the Wails shell (the Popup port of the recording prompt). It records
// the commands and whether the popup is visible, and is safe for concurrent use. A failing command changes nothing.
type FakePopup struct {
	mu      sync.Mutex
	visible bool
	shows   int
	hides   int
	err     error
}

// ShowPopup makes the popup visible.
func (f *FakePopup) ShowPopup() error { return f.command(true) }

// HidePopup hides the popup.
func (f *FakePopup) HidePopup() error { return f.command(false) }

func (f *FakePopup) command(show bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if show {
		f.shows++
	} else {
		f.hides++
	}
	if f.err != nil {
		return f.err
	}
	f.visible = show
	return nil
}

// Visible reports whether the last successful command showed the popup.
func (f *FakePopup) Visible() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.visible
}

// Shows returns how many times ShowPopup was called, including failed calls.
func (f *FakePopup) Shows() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shows
}

// Hides returns how many times HidePopup was called, including failed calls.
func (f *FakePopup) Hides() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hides
}

// Fail makes every later command return err, like a shell that does not run; nil heals the fake.
func (f *FakePopup) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}
