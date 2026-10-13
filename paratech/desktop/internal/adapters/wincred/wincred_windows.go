//go:build windows

package wincred

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"paratech/desktop/internal/domain/shared"
)

// x/sys/windows has no Cred* wrappers, so they are loaded lazily from advapi32 (read-only after init).
var (
	modAdvapi32    = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW = modAdvapi32.NewProc("CredWriteW")
	procCredReadW  = modAdvapi32.NewProc("CredReadW")
	procCredDelete = modAdvapi32.NewProc("CredDeleteW")
	procCredFree   = modAdvapi32.NewProc("CredFree")
)

const (
	// credTypeGeneric is CRED_TYPE_GENERIC: an application-defined blob.
	credTypeGeneric = 1

	// credPersistLocalMachine is CRED_PERSIST_LOCAL_MACHINE: survives logoff, never roams to other computers.
	credPersistLocalMachine = 2
)

// errNoSession is ERROR_NO_SUCH_LOGON_SESSION: the session has no credential set (service or network logon).
var errNoSession = fmt.Errorf("%w: no credential set in this logon session", shared.ErrStorage)

// credential mirrors CREDENTIALW (wincred.h); Go aligns it like C (80 bytes on 64-bit, 52 on 32-bit).
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// winBackend calls Credential Manager of the current user.
type winBackend struct{}

func newBackend() backend { return winBackend{} }

func (winBackend) write(target string, blob []byte) error {
	t, err := targetPtr(target)
	if err != nil {
		return err
	}
	if err := findProcs(); err != nil {
		return err
	}
	// Same limits as CheckSecret, on the length only: converting blob to a string would copy the secret.
	switch {
	case len(blob) == 0:
		return ErrEmptySecret
	case len(blob) > MaxSecretBytes:
		return ErrSecretTooLarge
	}

	c := credential{
		Type:               credTypeGeneric,
		TargetName:         t,
		CredentialBlobSize: uint32(len(blob)), //nolint:gosec // bounded by MaxSecretBytes above
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
	}
	r, _, e := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0) //nolint:gosec // syscall needs the struct address
	if r == 0 {
		return mapErrno("write", e)
	}
	return nil
}

// read copies the blob out and wipes the copy owned by Windows before freeing it.
func (winBackend) read(target string) ([]byte, error) {
	t, err := targetPtr(target)
	if err != nil {
		return nil, err
	}
	if err := findProcs(); err != nil {
		return nil, err
	}

	var c *credential
	r, _, e := procCredReadW.Call(
		uintptr(unsafe.Pointer(t)), //nolint:gosec // syscall needs the string address
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&c)), //nolint:gosec // CredReadW writes the buffer address here
	)
	if r == 0 {
		return nil, mapErrno("read", e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c))) //nolint:errcheck,gosec // CredFree returns nothing

	n := int(c.CredentialBlobSize)
	if n == 0 || c.CredentialBlob == nil {
		return []byte{}, nil
	}
	native := unsafe.Slice(c.CredentialBlob, n)
	out := make([]byte, n)
	copy(out, native)
	clear(native)
	return out, nil
}

func (winBackend) remove(target string) error {
	t, err := targetPtr(target)
	if err != nil {
		return err
	}
	if err := findProcs(); err != nil {
		return err
	}

	r, _, e := procCredDelete.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0) //nolint:gosec // syscall needs the string address
	if r == 0 {
		return mapErrno("delete", e)
	}
	return nil
}

// targetPtr converts the target to UTF-16; names are validated, so only a NUL could fail here.
func targetPtr(target string) (*uint16, error) {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return nil, ErrInvalidName
	}
	return t, nil
}

// findProcs resolves the procs up front: LazyProc.Call panics on a missing proc.
func findProcs() error {
	for _, p := range []*windows.LazyProc{procCredWriteW, procCredReadW, procCredDelete, procCredFree} {
		if p.Find() != nil {
			return fmt.Errorf("%w: credential manager API not available", shared.ErrStorage)
		}
	}
	return nil
}

// mapErrno turns the Windows error into a shared sentinel. The text carries neither the target nor the secret.
func mapErrno(op string, err error) error {
	var errno windows.Errno
	if !errors.As(err, &errno) {
		return fmt.Errorf("%w: credential manager %s failed", shared.ErrStorage, op)
	}

	switch errno {
	case windows.ERROR_NOT_FOUND:
		return fmt.Errorf("%w: credential", shared.ErrNotFound)
	case windows.ERROR_ACCESS_DENIED:
		return fmt.Errorf("%w: credential manager %s", shared.ErrStoragePermission, op)
	case windows.ERROR_NO_SUCH_LOGON_SESSION:
		return fmt.Errorf("credential manager %s: %w", op, errNoSession)
	}
	return fmt.Errorf("%w: credential manager %s failed with code %d", shared.ErrStorage, op, uint32(errno))
}
