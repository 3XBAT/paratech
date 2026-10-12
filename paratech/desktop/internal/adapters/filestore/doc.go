// Package filestore is the file artifact adapter: it implements the ArtifactStore ports of the use cases
// (Open, Write, List, Size, Delete) on a private data root (hard rule 5, ARCH-30..33).
//
// The root holds one directory per shared.ArtifactKind: recordings, transcripts, models, exports. Callers pass only
// shared.ArtifactRef, never OS paths. All access goes through os.Root, so no operation can leave the root, and
// any link (symlink or Windows junction) on the way is refused, even one that stays inside.
//
// Write is atomic: a temporary file in the target directory, fsync, rename. Names with the temporary prefix are
// reserved: they are rejected as references and hidden from List. Call CleanupTemp once at startup to remove
// leftovers of a crashed write.
//
// Errors are shared.ErrNotFound, shared.ErrInvalidArtifactRef, shared.ErrConflict (the entry has the wrong kind, for
// example a directory where a file is needed) or ErrStorage for other I/O failures. Raw os errors and OS paths do not
// cross the boundary. The root should lie in a per-user directory (%LOCALAPPDATA%); the adapter creates directories
// with mode 0700 and files with 0600 but does not manage Windows ACLs.
package filestore
