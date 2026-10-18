package wincred

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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

// ProfileFor returns the profile of dataDir. It is DefaultProfile only when dataDir and defaultDir are the same
// directory by file identity (volume and file id), so other spellings of it (case, junctions, symlinks, 8.3 names)
// match and no name that only folds to it does. Any doubt (defaultDir empty, either directory not found) gives an
// isolated profile, never the default one. Call it once dataDir exists.
//
// The id of an isolated profile hashes the normalized real path: absolute, symlinks and junctions followed, 8.3
// short names expanded and, on Windows, the case folded, so two spellings of one directory give one profile.
func ProfileFor(dataDir, defaultDir string) Profile {
	if defaultDir != "" && sameDir(dataDir, defaultDir) {
		return DefaultProfile
	}
	dir, err := resolveDir(dataDir)
	if err != nil {
		return profileOf(lexicalDir(dataDir))
	}
	return profileOf(dir)
}

// sameDir reports whether a and b are one existing directory; false when either cannot be read.
func sameDir(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil || !ia.IsDir() {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil || !ib.IsDir() {
		return false
	}
	return os.SameFile(ia, ib)
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
