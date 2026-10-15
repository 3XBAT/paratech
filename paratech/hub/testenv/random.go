package testenv

import "crypto/rand"

// Random is the random source of the use case ports for tests. It draws real random bytes, so the API keys of tests
// are generated at run time and never committed. A non-nil Err makes every Fill fail.
type Random struct {
	Err error
}

// Fill fills p with random bytes, or returns r.Err.
func (r Random) Fill(p []byte) error {
	if r.Err != nil {
		return r.Err
	}
	_, err := rand.Read(p)
	return err
}

// ZeroRandom is a broken random source that "succeeds" with all-zero bytes.
type ZeroRandom struct{}

// Fill sets p to zeros.
func (ZeroRandom) Fill(p []byte) error {
	clear(p)
	return nil
}
