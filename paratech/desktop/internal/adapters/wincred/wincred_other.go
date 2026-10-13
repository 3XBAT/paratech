//go:build !windows

package wincred

// otherBackend fails every call: there is no secure store wired off Windows, and a silent fallback would hide that.
type otherBackend struct{}

func newBackend() backend { return otherBackend{} }

func (otherBackend) write(string, []byte) error  { return ErrUnsupported }
func (otherBackend) read(string) ([]byte, error) { return nil, ErrUnsupported }
func (otherBackend) remove(string) error         { return ErrUnsupported }
