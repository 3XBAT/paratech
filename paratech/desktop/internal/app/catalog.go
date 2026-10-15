package app

import (
	"context"
	"embed"
	"fmt"
)

// catalogFile is the model catalog manifest shipped with the binary, in the format of values.ParseCatalog.
const catalogFile = "catalog.json"

// catalogFiles is read-only embedded data fixed at build time, not mutable state (ARCH-53); go:embed requires a
// package-level variable.
//
//go:embed catalog.json
var catalogFiles embed.FS

// embeddedCatalog is the catalog source of the model manager (PRD section 8.1). The manifest lists no model yet: no
// SHA-256 of a ggml model is verified, so the catalog is empty, download has nothing to offer and a model is
// installed by manual import only. Adding entries with verified digests to catalog.json is a follow-up. The use case
// parses and validates the bytes, so this is configuration data only.
type embeddedCatalog struct{}

// Manifest returns a fresh copy of the embedded manifest.
func (embeddedCatalog) Manifest(context.Context) ([]byte, error) {
	data, err := catalogFiles.ReadFile(catalogFile)
	if err != nil {
		return nil, fmt.Errorf("app: model catalog: %w", err) // unreachable: the file is embedded at build time
	}
	return data, nil
}
