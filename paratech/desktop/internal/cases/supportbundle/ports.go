package supportbundle

import (
	"context"
	"runtime/debug"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// ArchiveWriter writes the bundle to the chosen path (the zipfile adapter): fill adds the entries; on any failure
// nothing is left under the path.
type ArchiveWriter interface {
	WriteZip(ctx context.Context, path string, fill func(add func(name string, data []byte) error) error) error
}

// LogFiles reads the app's log files back, newest first, within maxBytes in all (the logfile adapter).
type LogFiles interface {
	EachLog(ctx context.Context, maxBytes int64, fn func(name string, data []byte) error) error
}

// SystemInfo tells the Windows version, the build of this executable and the user's profile directory (the sysinfo
// adapter).
type SystemInfo interface {
	Windows(ctx context.Context) values.WindowsVersion
	Build() (*debug.BuildInfo, bool)
	ProfileDir() string
}

// SettingsReader reads the settings (the sqlite adapter).
type SettingsReader interface {
	Load(ctx context.Context) (*settings.Settings, error)
}

// CaptureHistory returns the diagnostics of the recent captures of this run (the recorder adapter); none after a
// restart.
type CaptureHistory interface {
	RecentCaptures(ctx context.Context) []values.CaptureDiagnostics
}

// Clock supplies the time of the bundle (ARCH-24), in the student's zone: it names the file.
type Clock interface {
	Now() time.Time
}
