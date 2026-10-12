// Package sttmodel is the aggregate of one installed speech-to-text model (PRD section 8, AC-07, AC-08, AC-23).
//
// A model is a verified file below models/ with its identity (name and model version; the pair is unique among
// installed models), size, SHA-256, quantization and capabilities, and whether it came from the catalog. The source
// URL is not part of the aggregate: it belongs to the catalog entry (values.CatalogEntry), and an imported model has
// none. The aggregate decides whether it may be removed (CheckRemoval). It does no I/O.
//
// The "default model" is not stored here but in the settings aggregate (a change of the default then touches
// one aggregate, ARCH-23).
package sttmodel
