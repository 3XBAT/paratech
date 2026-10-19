//go:build !windows

package hubclient

import (
	"errors"
	"syscall"
)

// connRefused reports a dial the host refused: the network works, the Hub does not listen.
func connRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
