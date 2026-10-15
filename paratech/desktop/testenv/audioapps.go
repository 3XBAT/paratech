package testenv

import (
	"context"
	"slices"
	"sync"

	"paratech/desktop/internal/domain/recording/values"
)

// FakeAudioApps is the list of open apps of the source picker (the AudioApps port of the recording prompt): what Set
// gave it, or the error of Fail. The zero value lists no app.
type FakeAudioApps struct {
	mu    sync.Mutex
	apps  []values.AudioApp
	err   error
	lists int
}

// Set makes later List calls return apps.
func (f *FakeAudioApps) Set(apps ...values.AudioApp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = slices.Clone(apps)
}

// Fail makes later List calls fail with err; nil heals it.
func (f *FakeAudioApps) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// List returns a copy of the apps, or the error of Fail.
func (f *FakeAudioApps) List(ctx context.Context) ([]values.AudioApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return slices.Clone(f.apps), nil
}

// Lists returns how many times List was called.
func (f *FakeAudioApps) Lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}
