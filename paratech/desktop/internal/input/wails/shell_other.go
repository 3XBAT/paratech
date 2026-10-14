//go:build !windows

package wails

import "context"

// run reports that there is no UI shell: the product targets Windows only, other systems run headless.
func (s *Shell) run(context.Context) error { return ErrUnsupported }
