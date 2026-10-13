// Package modelcatalog is the use case of managing local STT models: the catalog, installed models, registration of a
// verified file, the default model, pinning and safe removal (PRD sections 8 and 8.1, AC-07, AC-08, AC-23).
//
// Service (TASK-026) lists the catalog (CatalogSource, parsed and validated by values.ParseCatalog) and the installed
// models, registers a model file that a download or import flow stored (hashing it itself), selects the default through the
// settings aggregate, resolves the exact model an STT job pins and removes a model only when no unfinished job pins
// it (ADR-0004(g)). Resumable download, the disk-space check and the import of a user file (TASK-027, TASK-028) add
// their own ports and methods to this package.
//
// Rules that apply: ARCH-20..27.
package modelcatalog
