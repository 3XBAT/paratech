//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package instancelock

import "os"

func lockFile(*os.File) error { return ErrUnsupported }

func unlockFile(*os.File) error { return nil }
