package values

// AppBuild identifies the build of this executable for diagnostics, as the Go toolchain embeds it: the module path
// and version ("(devel)" for a local build), the VCS revision, its time (RFC 3339) and whether the tree had local
// changes, the Go version and the target ("windows/amd64"). The zero value means none is known.
type AppBuild struct {
	Module       string
	Version      string
	Revision     string
	RevisionTime string
	Modified     bool
	GoVersion    string
	OS           string
}

// IsZero reports whether nothing is known.
func (b AppBuild) IsZero() bool { return b == AppBuild{} }
