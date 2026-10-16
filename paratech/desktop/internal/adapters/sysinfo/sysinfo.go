// Package sysinfo reads what a support bundle tells about the machine and the build (TASK-058, AC-4): the Windows
// version (RtlGetVersion and the read-only registry texts), the build information of this executable and the user's
// profile directory, which the bundle hides in its texts. Nothing here is written or sent anywhere.
package sysinfo

import (
	"context"
	"runtime/debug"

	"paratech/desktop/internal/domain/recording/values"
)

// Reader reads the facts. The zero value is ready to use.
type Reader struct{}

// New returns a Reader.
func New() Reader { return Reader{} }

// Build returns the build information embedded in this executable (module version, VCS revision and time, Go
// version, target); zero when the binary has none.
func (Reader) Build() values.AppBuild {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return values.AppBuild{}
	}
	return buildOf(info)
}

// buildOf keeps the facts of info a support bundle tells; the other build settings (flags, paths) stay out.
func buildOf(info *debug.BuildInfo) values.AppBuild {
	if info == nil {
		return values.AppBuild{}
	}
	b := values.AppBuild{Module: info.Main.Path, Version: info.Main.Version, GoVersion: info.GoVersion}
	var goos, goarch string
	for _, kv := range info.Settings {
		switch kv.Key {
		case "vcs.revision":
			b.Revision = kv.Value
		case "vcs.time":
			b.RevisionTime = kv.Value
		case "vcs.modified":
			b.Modified = kv.Value == "true"
		case "GOOS":
			goos = kv.Value
		case "GOARCH":
			goarch = kv.Value
		}
	}
	if goos != "" {
		b.OS = goos + "/" + goarch
	}
	return b
}

// Windows returns the Windows version; zero outside Windows. ctx is for the port contract: the reads are local.
func (Reader) Windows(context.Context) WindowsVersion { return windowsVersion() }

// ProfileDir returns the profile directory of the user running this process ("C:\Users\name"), "" when unknown.
func (Reader) ProfileDir() string { return profileDir() }
