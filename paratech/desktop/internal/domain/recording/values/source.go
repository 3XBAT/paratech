package values

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// ErrInvalidSource means a recording source cannot be built from the given app name.
var ErrInvalidSource = errors.New("invalid recording source")

// Source is what a recording captures (ADR-0002 Decision 1): one app, chosen by its executable name and captured with
// its process tree, or the whole computer except this app. The zero value is the whole computer.
type Source struct {
	app shared.AppName
}

// WholeComputer is the fallback source: everything the computer plays except this app.
func WholeComputer() Source { return Source{} }

// SourceOf is the source for app; the empty name is the whole computer.
func SourceOf(app shared.AppName) (Source, error) {
	if app.IsZero() {
		return WholeComputer(), nil
	}
	if err := app.Validate(); err != nil {
		return Source{}, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	return Source{app: app}, nil
}

// App returns the chosen app; empty for the whole computer.
func (s Source) App() shared.AppName { return s.app }

// IsWholeComputer reports whether the source is the whole computer.
func (s Source) IsWholeComputer() bool { return s.app.IsZero() }

// String describes the source for logs: "whole computer" or "app chrome.exe".
func (s Source) String() string {
	if s.IsWholeComputer() {
		return "whole computer"
	}
	return "app " + s.app.String()
}
