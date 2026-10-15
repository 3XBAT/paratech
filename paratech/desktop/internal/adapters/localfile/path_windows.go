//go:build windows

package localfile

import (
	"fmt"
	"path/filepath"
	"strings"

	"paratech/desktop/internal/domain/transcription/values"
)

// checkLocal accepts only a cleaned path on a drive letter. A UNC path (\\server\share) makes Windows contact the
// host over SMB and send the user's NTLM credentials; device and namespace paths (\\.\, \\?\, \??\) reach devices
// and volumes. Reserved device names (NUL, COM1, CONIN$) and alternate data streams (file:stream) are refused too.
func checkLocal(clean string) error {
	vol := filepath.VolumeName(clean)
	if !isDriveLetter(vol) {
		return fmt.Errorf("%w: not a path on a local drive", values.ErrInvalidImportFile)
	}

	// IsLocal refuses colons and reserved names in every element of the rest of the path.
	rest := strings.TrimLeft(clean[len(vol):], `\`)
	if rest == "" || !filepath.IsLocal(rest) {
		return fmt.Errorf("%w: a device name or a stream", values.ErrInvalidImportFile)
	}
	return nil
}

func isDriveLetter(vol string) bool {
	if len(vol) != 2 || vol[1] != ':' {
		return false
	}
	c := vol[0] | 0x20 // lower case
	return 'a' <= c && c <= 'z'
}
