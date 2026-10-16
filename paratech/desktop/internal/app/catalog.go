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

// embeddedCatalog is the catalog source of the model manager (PRD section 8.1): the recommended whisper.cpp models
// with the sizes and SHA-256 of the official Hugging Face metadata, pinned to one repository commit
// (docs/refs/whisper.md, "Model catalog"). The use case parses and validates the bytes, so this is configuration data
// only.
type embeddedCatalog struct{}

// Manifest returns a fresh copy of the embedded manifest.
func (embeddedCatalog) Manifest(context.Context) ([]byte, error) {
	data, err := catalogFiles.ReadFile(catalogFile)
	if err != nil {
		return nil, fmt.Errorf("app: model catalog: %w", err) // unreachable: the file is embedded at build time
	}
	return data, nil
}
