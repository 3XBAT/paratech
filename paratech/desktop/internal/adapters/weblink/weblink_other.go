//go:build !windows

package weblink

import "context"

// start has no browser launcher off Windows: the product targets Windows only.
func start(context.Context, string) error { return ErrUnsupported }

// startShortcut has no browser launcher off Windows either.
func startShortcut(context.Context, string) error { return ErrUnsupported }
