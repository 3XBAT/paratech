//go:build !windows

package localfile

// checkLocal has nothing to refuse beyond an absolute cleaned path: other systems have no UNC paths.
func checkLocal(string) error { return nil }
