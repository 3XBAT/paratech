//go:build !windows

package wasapi

// Processes is not implemented outside Windows.
func Processes() ([]Proc, error) { return nil, ErrUnsupported }

// FindTreeRoot is not implemented outside Windows.
func FindTreeRoot(string) (uint32, error) { return 0, ErrUnsupported }
