//go:build !windows

package filestore

import (
	"io/fs"
	"syscall"

	"paratech/desktop/internal/domain/shared"
)

func platformStorageCases(path string) []struct {
	name string
	err  error
	is   []error
	not  []error
} {
	return []struct {
		name string
		err  error
		is   []error
		not  []error
	}{
		{"disk full", &fs.PathError{Op: "write", Path: path, Err: syscall.ENOSPC}, []error{shared.ErrStorageFull, syscall.ENOSPC, ErrStorage}, []error{shared.ErrStorageBusy}},
		{"busy", &fs.PathError{Op: "rename", Path: path, Err: syscall.EBUSY}, []error{shared.ErrStorageBusy, ErrStorage}, []error{shared.ErrStorageFull}},
	}
}
