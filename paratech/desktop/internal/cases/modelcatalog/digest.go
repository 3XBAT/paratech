package modelcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// digestFile streams the artifact through SHA-256 (PRD section 8.1, section 14 case 13). It runs outside any
// transaction, stops when ctx ends, and refuses a file whose length is not size: the file changed after its size
// was read.
func (s *Service) digestFile(ctx context.Context, ref shared.ArtifactRef, size int64) (sum values.SHA256, err error) {
	rc, err := s.files.Open(ctx, ref)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, rc.Close()) }()

	h := sha256.New()
	n, err := io.Copy(h, ctxReader{ctx: ctx, r: rc})
	if err != nil {
		return "", err
	}
	if n != size {
		return "", fmt.Errorf("%w: %s changed while it was hashed: %d bytes, then %d", values.ErrSizeMismatch, ref, size, n)
	}

	return values.SHA256(hex.EncodeToString(h.Sum(nil))), nil
}

// ctxReader ends a long read (a model file has gigabytes) as soon as ctx ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
