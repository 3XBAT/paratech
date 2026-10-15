package app

// Stand-ins for the ports that this build cannot configure. Each one fails with a fixed text that wraps
// errNotConfigured.

import (
	"context"
	"errors"
	"fmt"
)

// errNotConfigured marks a call into a part of the core that this build does not configure.
var errNotConfigured = errors.New("not configured in this build")

// catalogNotConfigured is the model catalog manifest of a build without one: there is no verified SHA-256 of any
// downloadable model yet (hard rule 9), and values.ParseCatalog refuses a manifest without entries. Listing the
// catalog, downloading and importing fail with this error; the installed models, the default and the pin of an STT
// job (ResolveForJob) do not read the catalog and work.
type catalogNotConfigured struct{}

func (catalogNotConfigured) Manifest(context.Context) ([]byte, error) {
	return nil, fmt.Errorf("model catalog: %w", errNotConfigured)
}
