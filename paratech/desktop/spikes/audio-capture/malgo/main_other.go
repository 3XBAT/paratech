//go:build !(windows && cgo)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "malgospike: needs windows and CGO_ENABLED=1")
	os.Exit(1)
}
