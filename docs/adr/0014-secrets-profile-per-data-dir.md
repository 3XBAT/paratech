# ADR-0014: Credential Manager secrets per data directory

- **Status:** Accepted
- **Date:** 2026-10-18
- **Source:** TASK-071; PRD §12, ADR-0011.

## Context
Credential Manager is global per Windows user. A test run with a temporary `--data-dir` read the owner's real Google
sign-in and started a calendar sync; the smoke run of `build-alpha.ps1` (`smoke-data`) could do the same.

## Decision
- **Names.** The default data dir (`%LOCALAPPDATA%\LearningAssistant`, kept by the rename, ADR-0015) keeps `paratech/<service>/<name>`; no migration.
  Any other dir uses `paratech-<profile>/<service>/<name>`, profile = first 16 hex chars of SHA-256 of the normalized path.
  `-` instead of `/` and the fixed length mean no name of one profile equals or starts with a name prefix of another.
- **Default or not** (`wincred.ProfileFor`, after the app has created and locked the dir): the default names only when
  the data dir and the default dir are the same directory by file identity (`os.SameFile`: volume + file id), so case,
  a trailing separator, junctions, symlinks and 8.3 names match, and a name that only folds to it (`İ`, Kelvin `K`) does not.
  The explicit `--data-dir` of the default dir (as the recorder child gets) is the default profile.
- **Profile id**: hash of the absolute final path from Windows (`GetFinalPathNameByHandle`), no trailing separator, upper case.
- **Fail closed.** An unknown default dir, or either dir that cannot be read, gives an isolated profile.
- The startup log says `secrets_profile=default` or `secrets_profile=isolated` at info; the smoke run requires `isolated`.
  The id (`secrets_profile_id`) is only at debug level: it hashes a predictable path with the user name in it.

## Consequences
- Another data dir starts with no Hub key, mail password or Google client; they are entered there again.
- Deleting a temporary data dir leaves its `paratech-<profile>/...` entries; remove them by name in Credential Manager
  (Windows Credentials) or with `cmdkey /delete:LegacyGeneric:target=paratech-<profile>/mail/smtp-password` (the name is
  everything after `Target:` in `cmdkey /list`). There is no tool for it.
- A non-default dir that is renamed or moved gets another profile (its secrets are entered again).
