//go:build windows

package hubclient

import (
	"errors"
	"syscall"
)

// wsaeConnRefused is WSAECONNREFUSED: on Windows a refused dial carries this Winsock code, which the syscall package
// does not export and which is not syscall.ECONNREFUSED.
const wsaeConnRefused = syscall.Errno(10061)

// connRefused reports a dial the host refused: the network works, the Hub does not listen.
func connRefused(err error) bool {
	return errors.Is(err, wsaeConnRefused) || errors.Is(err, syscall.ECONNREFUSED)
}
