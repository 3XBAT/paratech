// Package filestore is the file artifact adapter: it implements the ArtifactStore ports of the use cases
// (Open, Write, List, Size, Delete) on a private data root (hard rule 5, ARCH-30..33).
//
// The root holds one directory per shared.ArtifactKind: recordings, transcripts, models, exports. Callers pass only
// shared.ArtifactRef, never OS paths. All access goes through os.Root, so no operation can leave the root, and
// any link (symlink or Windows junction) on the way is refused, even one that stays inside.
//
// Write is atomic: a temporary file in the target directory, fsync, rename. Names with the temporary prefix are
// reserved: they are rejected as references and hidden from List. Call CleanupTemp once at startup, before any
// Write, to remove leftovers of a crashed write. On Windows, close readers of a file before overwriting it.
//
// Errors are shared.ErrNotFound, shared.ErrInvalidArtifactRef, shared.ErrConflict (the entry has the wrong kind, for
// example a directory where a file is needed) or shared.ErrStorage for other I/O failures, narrowed where the OS
// error allows to shared.ErrStoragePermission, ErrStorageFull or ErrStorageBusy. Write never returns ErrNotFound.
// OS paths do not appear in messages and *fs.PathError is not exposed; the bare OS error stays reachable through
// errors.Is. The root should lie in a per-user directory (%LOCALAPPDATA%); the adapter creates directories
// with mode 0700 and files with 0600 but does not manage Windows ACLs.
package filestore
