package wincred

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// ProfileLen is the length of an isolated profile id: lowercase hex of the start of a SHA-256.
const ProfileLen = 16

// Profile selects the set of target names of a data directory (ADR-0014). Credential Manager is global per Windows
// user, so without it a run on a temporary data directory would use the secrets of the real one.
type Profile string

// DefaultProfile is the profile of the default data directory: the target names without a profile part.
const DefaultProfile Profile = ""

// ErrInvalidProfile means a profile is neither DefaultProfile nor ProfileLen lowercase hex digits.
var ErrInvalidProfile = fmt.Errorf("%w: credential profile", shared.ErrInvalidID)

// CheckProfile reports whether p is DefaultProfile or a well-formed isolated profile.
func CheckProfile(p Profile) error {
	if p == DefaultProfile {
		return nil
	}
	if len(p) != ProfileLen {
		return ErrInvalidProfile
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ErrInvalidProfile
		}
	}
	return nil
}

// Isolated reports whether p is not the default profile.
func (p Profile) Isolated() bool { return p != DefaultProfile }

// ProfileFor returns the profile of dataDir. It is DefaultProfile only when dataDir and defaultDir resolve to the same
// directory; any doubt (defaultDir empty or unresolvable, dataDir unresolvable) gives an isolated profile, never the
// default one. Call it once dataDir exists: resolving needs the directory.
//
// Resolving makes the path absolute, follows symlinks and junctions, expands 8.3 short names and, on Windows, folds
// the case, so two spellings of one directory give one profile.
func ProfileFor(dataDir, defaultDir string) Profile {
	dir, err := resolveDir(dataDir)
	if err != nil {
		return profileOf(lexicalDir(dataDir))
	}
	if defaultDir != "" {
		if def, err := resolveDir(defaultDir); err == nil && def == dir {
			return DefaultProfile
		}
	}
	return profileOf(dir)
}

// resolveDir is the normalized real path of an existing directory.
func resolveDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := realPath(abs)
	if err != nil {
		return "", err
	}
	return foldPath(trimSeparators(resolved)), nil
}

// lexicalDir is the normalized path without touching the file system, the fallback for an unresolvable directory.
func lexicalDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	return foldPath(trimSeparators(abs))
}

func trimSeparators(path string) string {
	return strings.TrimRight(path, string(filepath.Separator))
}

func profileOf(normalized string) Profile {
	sum := sha256.Sum256([]byte(normalized))
	return Profile(hex.EncodeToString(sum[:])[:ProfileLen])
}
