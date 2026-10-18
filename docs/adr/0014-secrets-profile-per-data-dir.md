# ADR-0014: Credential Manager secrets per data directory

- **Status:** Accepted
- **Date:** 2026-10-18
- **Source:** TASK-071; PRD §12, ADR-0011.

## Context
Credential Manager is global per Windows user. A test run with a temporary `--data-dir` read the owner's real Google
sign-in and started a calendar sync; the smoke run of `build-alpha.ps1` (`smoke-data`) could do the same.

## Decision
- **Names.** The default data dir (`%LOCALAPPDATA%\LearningAssistant`) keeps `paratech/<service>/<name>`; no migration.
  Any other dir uses `paratech-<profile>/<service>/<name>`, profile = first 16 hex chars of SHA-256 of the normalized path.
  `-` instead of `/` and the fixed length mean no name of one profile equals or starts with a name prefix of another.
- **Normalization** (`wincred.ProfileFor`, after the app has created and locked the dir): absolute path, then the final path
  from Windows (`GetFinalPathNameByHandle`: junctions, symlinks, 8.3 names, mapped drives), no trailing separator, lower case.
  The explicit `--data-dir` of the default dir (as the recorder child gets) is the default profile.
- **Fail closed.** Default names only when both paths resolve and are equal; an unknown or unresolvable default dir, or a
  data dir that does not resolve, gives an isolated profile.
- The startup log says `secrets_profile=default` or `secrets_profile=isolated secrets_profile_id=<profile>`; the smoke run
  requires `isolated`.

## Consequences
- Another data dir starts with no Hub key, mail password or Google client; they are entered there again.
- Deleting a temporary data dir leaves its `paratech-<profile>/...` entries; remove them by name in Credential Manager
  (Windows Credentials) or with `cmdkey /delete`. There is no tool for it.
- A non-default dir that is renamed or moved gets another profile (its secrets are entered again).
