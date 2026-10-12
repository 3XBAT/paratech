package filestore

import (
	"io/fs"

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
		{"disk full", &fs.PathError{Op: "write", Path: path, Err: errDiskFull}, []error{shared.ErrStorageFull, ErrStorage}, []error{shared.ErrStorageBusy}},
		{"sharing violation", &fs.PathError{Op: "rename", Path: path, Err: errSharingViolation}, []error{shared.ErrStorageBusy, ErrStorage}, []error{shared.ErrStorageFull}},
	}
}
