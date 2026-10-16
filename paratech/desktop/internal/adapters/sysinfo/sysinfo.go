// Package sysinfo reads what a support bundle tells about the machine and the build (TASK-058, AC-4): the Windows
// version (RtlGetVersion and the read-only registry texts), the build information of this executable and the user's
// profile directory, which the bundle hides in its texts. Nothing here is written or sent anywhere.
package sysinfo

import (
	"context"
	"runtime/debug"
)

// Reader reads the facts. The zero value is ready to use.
type Reader struct{}

// New returns a Reader.
func New() Reader { return Reader{} }

// Build returns the build information embedded in this executable (module version, VCS revision and time, Go
// version); ok is false when the binary has none.
func (Reader) Build() (*debug.BuildInfo, bool) { return debug.ReadBuildInfo() }

// Windows returns the Windows version; zero outside Windows. ctx is for the port contract: the reads are local.
func (Reader) Windows(context.Context) WindowsVersion { return windowsVersion() }

// ProfileDir returns the profile directory of the user running this process ("C:\Users\name"), "" when unknown.
func (Reader) ProfileDir() string { return profileDir() }
