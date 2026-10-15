// Package random is the cryptographically secure random source behind the Random ports of the use cases (ARCH-24).
package random

import "crypto/rand"

// Source reads crypto/rand. The zero value is ready to use.
type Source struct{}

// Fill fills p with random bytes.
func (Source) Fill(p []byte) error {
	_, err := rand.Read(p)
	return err
}
