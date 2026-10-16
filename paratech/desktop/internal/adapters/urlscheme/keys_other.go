//go:build !windows

package urlscheme

import "errors"

// platformKeys refuse every call: there is no Windows registry here.
type platformKeys struct{}

func (platformKeys) value(string, string) (string, bool, error) {
	return "", false, errors.ErrUnsupported
}

func (platformKeys) setValue(string, string, string) error { return errors.ErrUnsupported }

func (platformKeys) has(string, string) (bool, error) { return false, errors.ErrUnsupported }

func (platformKeys) deleteKey(string) error { return errors.ErrUnsupported }
