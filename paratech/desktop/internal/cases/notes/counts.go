package notes

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const opPartCounts = "notes: text of a part not counted"

// Bounds of the counts of the parts (TASK-253).
const (
	// maxCountReads bounds the texts one view or listing reads: each read hashes the whole file again, so a subject
	// with many notes is counted over several listings. One full note always fits.
	maxCountReads = 200

	// maxCachedCounts bounds the counts kept in memory; a full cache is dropped and filled again.
	maxCachedCounts = 4096
)

// countCache keeps the counts of a text by the sha256 of its file: the content of a file never changes, so a count
// is read once per run.
type countCache struct {
	mu       sync.Mutex
	byDigest map[material.Digest]material.TextCounts
}

func newCountCache() *countCache {
	return &countCache{byDigest: map[material.Digest]material.TextCounts{}}
}

func (c *countCache) get(d material.Digest) (material.TextCounts, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	got, ok := c.byDigest[d]
	return got, ok
}

func (c *countCache) put(d material.Digest, counts material.TextCounts) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.byDigest) >= maxCachedCounts {
		clear(c.byDigest)
	}
	c.byDigest[d] = counts
}

// countBudget is the number of texts a view or listing may still read.
type countBudget struct{ left int }

func newCountBudget() *countBudget { return &countBudget{left: maxCountReads} }

func (b *countBudget) take() bool {
	if b.left == 0 {
		return false
	}
	b.left--
	return true
}

// partCounts tells how the text of the part m is cut. A part without text, gone, removed, damaged or unread (the
// store failed, the budget is used up) counts zero: the counts never fail a view.
func (s *Service) partCounts(ctx context.Context, m *material.Material, b *countBudget) material.TextCounts {
	if m == nil || !m.Active() || !m.Type().Readable() {
		return material.TextCounts{}
	}
	if c, ok := s.counts.get(m.Digest()); ok {
		return c
	}
	if !b.take() {
		return material.TextCounts{}
	}

	_, text, err := s.materials.MaterialText(ctx, m.ID())
	switch {
	case err == nil:
	case errors.Is(err, material.ErrTextNotExtracted), errors.Is(err, material.ErrTextUnreadable):
		// The same bytes give the same answer: not read again.
		s.counts.put(m.Digest(), material.TextCounts{})
		return material.TextCounts{}
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved), errors.Is(err, material.ErrDamaged):
		return material.TextCounts{}
	default:
		if ctx.Err() == nil {
			s.log.Warn(opPartCounts, slog.String("material", string(m.ID())), slog.Any("cause", err))
		}
		return material.TextCounts{}
	}
	c := text.Counts()
	s.counts.put(m.Digest(), c)
	return c
}
