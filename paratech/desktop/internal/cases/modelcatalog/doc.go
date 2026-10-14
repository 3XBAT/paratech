// Package modelcatalog is the use case of managing local STT models: the catalog, installed models, registration of a
// verified file, the default model, pinning and safe removal (PRD sections 8 and 8.1, AC-07, AC-08, AC-23).
//
// Service (TASK-026) lists the catalog (CatalogSource, parsed and validated by values.ParseCatalog) and the installed
// models, registers a model file that a download or import flow stored (hashing it itself), selects the default through the
// settings aggregate, resolves the exact model an STT job pins and removes a model only when no unfinished job pins
// it (ADR-0004(g)).
//
// Downloader (TASK-027) downloads a catalog model: disk space check first, a resumable partial file below
// models/downloads, ranged requests to the hosts of the catalog allowlist only (ModelSource), progress for
// subscribers, the SHA-256 check, and Register on a fresh file name. The import of a user file (TASK-028) adds its own
// methods.
//
// Rules that apply: ARCH-20..27.
package modelcatalog
