package prompts

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
)

// files is read-only embedded data fixed at build time, not mutable state (ARCH-53); go:embed needs a package-level
// variable.
//
//go:embed agent ops
var files embed.FS

// File names inside a version directory.
const (
	systemFile = "system.md"
	toolsFile  = "tools.json"
	reaskFile  = "reask.md"
)

// ErrNotFound means there is no embedded version of the profile or operation.
var ErrNotFound = errors.New("prompts: not found")

// AgentVersion is one embedded version of an agent profile.
type AgentVersion struct {
	Version uint32
	System  []byte
	Tools   []byte
}

// Agent returns every embedded version of profile, oldest first. Every version directory must hold both files.
func Agent(profile string) ([]AgentVersion, error) {
	return agentIn(files, profile)
}

// Op returns the newest embedded server prompt of operation and its version; ErrNotFound when there is none.
func Op(operation string) (version uint32, system []byte, err error) {
	return opIn(files, operation)
}

// OpReask returns the reask note of the newest embedded version of operation and that version: the server text added
// to its prompt when the Hub asks the model once more after a refusal it can fix (ADR-0025). ErrNotFound when the
// operation or its newest version has none.
func OpReask(operation string) (version uint32, note []byte, err error) {
	return opReaskIn(files, operation)
}

func agentIn(fsys fs.FS, profile string) ([]AgentVersion, error) {
	versions, err := versionsIn(fsys, "agent", profile)
	if err != nil {
		return nil, err
	}
	out := make([]AgentVersion, 0, len(versions))
	for _, v := range versions {
		dir := path.Join("agent", profile, "v"+strconv.FormatUint(uint64(v), 10))
		system, err := fs.ReadFile(fsys, path.Join(dir, systemFile))
		if err != nil {
			return nil, fmt.Errorf("prompts: %s: %w", dir, err)
		}
		tools, err := fs.ReadFile(fsys, path.Join(dir, toolsFile))
		if err != nil {
			return nil, fmt.Errorf("prompts: %s: %w", dir, err)
		}
		out = append(out, AgentVersion{Version: v, System: system, Tools: tools})
	}
	return out, nil
}

func opIn(fsys fs.FS, operation string) (uint32, []byte, error) {
	versions, err := versionsIn(fsys, "ops", operation)
	if err != nil {
		return 0, nil, err
	}
	newest := versions[len(versions)-1]
	file := path.Join("ops", operation, "v"+strconv.FormatUint(uint64(newest), 10), systemFile)
	system, err := fs.ReadFile(fsys, file)
	if err != nil {
		return 0, nil, fmt.Errorf("prompts: %s: %w", file, err)
	}
	return newest, system, nil
}

func opReaskIn(fsys fs.FS, operation string) (uint32, []byte, error) {
	versions, err := versionsIn(fsys, "ops", operation)
	if err != nil {
		return 0, nil, err
	}
	newest := versions[len(versions)-1]
	file := path.Join("ops", operation, "v"+strconv.FormatUint(uint64(newest), 10), reaskFile)
	note, err := fs.ReadFile(fsys, file)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil, ErrNotFound
	}
	if err != nil {
		return 0, nil, fmt.Errorf("prompts: %s: %w", file, err)
	}
	return newest, note, nil
}

// versionsIn lists the version directories v1, v2, ... under kind/name in ascending order. Anything else in that
// directory is an error, so a misnamed version cannot be skipped silently.
func versionsIn(fsys fs.FS, kind, name string) ([]uint32, error) {
	if !validName(name) {
		return nil, fmt.Errorf("prompts: %s name must be snake_case", kind)
	}
	entries, err := fs.ReadDir(fsys, path.Join(kind, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("prompts: %s/%s: %w", kind, name, err)
	}
	var versions []uint32
	for _, e := range entries {
		v, ok := parseVersion(e.Name())
		if !e.IsDir() || !ok {
			return nil, fmt.Errorf("prompts: %s/%s/%s is not a version directory v<N>", kind, name, e.Name())
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		return nil, ErrNotFound
	}
	slices.Sort(versions)
	return versions, nil
}

// parseVersion reads "v<N>" with N in 1..2^32-1 without leading zeros.
func parseVersion(s string) (uint32, bool) {
	digits, ok := strings.CutPrefix(s, "v")
	if !ok || digits == "" || digits[0] == '0' {
		return 0, false
	}
	n, err := strconv.ParseUint(digits, 10, 32)
	return uint32(n), err == nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 'a' || c > 'z') && c != '_' {
			return false
		}
	}
	return true
}
