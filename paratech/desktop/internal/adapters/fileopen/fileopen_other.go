//go:build !windows

package fileopen

import "context"

// launch has no file launcher off Windows: the product targets Windows only.
func (o *Opener) launch(context.Context, string) error { return ErrUnsupported }

// runProcess is never reached off Windows.
func runProcess(context.Context, string, string, string) error { return ErrUnsupported }
