// Package localfile is the adapter that reads a file the user picked outside the data directory, for an import
// (ARCH-30..33). Two readers share one open:
//   - Reader implements modelcatalog.ImportSource: Open returns an io.ReadCloser of a model file (PRD sections 8 and
//     8.1), read once from the start.
//   - SeekReader implements recordingimport.Originals: Open returns an io.ReadSeekCloser of the user's original
//     audio file (ADR-0027 «Old recordings»), so the import reads its head, its tail and WAV chunk headers, then the
//     whole file for its digest and once more for the copy.
//
// Both open the file read-only and return it with its size; the caller closes it. The original is never written,
// moved or deleted: the import keeps a copy in the data directory, made through the Storage Manager.
//
// The path is untrusted input: it is cleaned and must be absolute and free of NUL bytes; on Windows it must be on a
// drive letter, so UNC, device and namespace paths, reserved device names and streams are refused. A mapped network
// drive and a symbolic link or junction the user picked are followed, even to a remote target. Only regular files
// are opened (no directories, devices or pipes). The file is checked again after the open, so a path swapped for
// another file in between is refused. Errors are values.ErrInvalidImportFile, shared.ErrNotFound or shared.ErrStorage
// (narrowed to shared.ErrStoragePermission when access is denied), also for reads and seeks; the OS path never
// appears in messages.
package localfile
