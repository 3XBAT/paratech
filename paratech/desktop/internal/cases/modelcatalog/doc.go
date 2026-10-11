// Package modelcatalog is the use case of managing local STT models: the catalog, installed models, import of a
// downloaded file, the default model, pinning and safe removal (PRD sections 8 and 8.1, AC-07, AC-08, AC-23).
//
// TASK-003 contains only the ports (ports.go) the model manager needs for registry and file storage. The Service
// arrives with TASK-026; resumable download, the disk-space check and the manifest source (TASK-027, TASK-028) add
// their own ports to this package.
//
// Rules that apply: ARCH-20..27.
package modelcatalog
