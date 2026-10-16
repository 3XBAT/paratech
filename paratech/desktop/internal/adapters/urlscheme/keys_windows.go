//go:build windows

package urlscheme

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// platformKeys are the values below HKEY_CURRENT_USER.
type platformKeys struct{}

func (platformKeys) value(path, name string) (string, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer func() { _ = k.Close() }()

	v, typ, err := k.GetStringValue(name)
	switch {
	case errors.Is(err, registry.ErrNotExist), errors.Is(err, registry.ErrUnexpectedType):
		return "", false, nil
	case err != nil:
		return "", false, err
	case typ != registry.SZ:
		return "", false, nil // REG_EXPAND_SZ would expand %...% in the command
	}
	return v, true, nil
}

func (platformKeys) setValue(path, name, v string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	return k.SetStringValue(name, v)
}
