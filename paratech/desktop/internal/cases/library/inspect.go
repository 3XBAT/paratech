package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the text cache log.
const (
	opTextCacheRead   = "library: cached text not read, extracted again"
	opTextCacheBroken = "library: cached text broken, extracted again"
	opTextCacheSave   = "library: text not cached"
)

// maxCachedTextBytes bounds a cached text read back; the cached form of a text within material.MaxTextBytes is far
// smaller, so a larger file is broken.
const maxCachedTextBytes = 4 * material.MaxTextBytes

// MaterialText reads the text of an active material (TASK-122): the stored bytes are read again and checked against
// the material's sha256, then the text cached for them is used, or the extractor reads them and the text is cached
// (TASK-150). It changes no material. The homework workflow reads the text of its material jobs through it (wired by
// the app).
//
// Errors (each also wraps the domain sentinel in brackets, for callers that cannot import this package):
// ErrMaterialNotFound [shared.ErrNotFound], ErrMaterialRemoved [material.ErrRemoved], ErrNoText
// [material.ErrTextNotExtracted] for a PDF or a photo, ErrFileDamaged [material.ErrDamaged], ErrTextUnreadable
// [material.ErrTextUnreadable], ErrUnavailable, ErrNotConfigured.
func (s *Service) MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	if s.texts == nil {
		return nil, material.Text{}, ErrNotConfigured
	}
	m, err := s.Material(ctx, id)
	switch {
	case err != nil:
		return nil, material.Text{}, err
	case !m.Active():
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrMaterialRemoved, material.ErrRemoved)
	case !m.Type().Readable(): // a PDF or a photo: never read, so never searched, inspected or cited
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrNoText, material.ErrTextNotExtracted)
	}

	ref, err := material.BlobRef(m.Digest())
	if err != nil {
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	data, err := s.readBlob(ctx, ref)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return m, material.Text{}, fmt.Errorf("%w: %w: %w", ErrFileDamaged, material.ErrDamaged, err)
	case err != nil:
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case material.DigestOf(data) != m.Digest():
		// TASK-121 follow-up: the store is trusted by name only on upload; reading checks the content.
		s.log.Warn(opBlobDamaged, slog.String("material", string(id)))
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrFileDamaged, material.ErrDamaged)
	}

	cache, err := material.TextRef(m.Digest())
	if err != nil {
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if text, ok := s.cachedText(ctx, id, cache); ok {
		return m, text, nil
	}

	text, err := s.texts.Extract(ctx, m.Type(), data)
	switch {
	case errors.Is(err, material.ErrTextNotExtracted):
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrNoText, err)
	case errors.Is(err, material.ErrTextUnreadable):
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrTextUnreadable, err)
	case err != nil:
		return m, material.Text{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	s.cacheText(ctx, id, cache, text)
	return m, text, nil
}

// cachedText reads the text cached for a file (ADR-0027 «Text»). The cache is derived: a missing one is a miss, a
// broken one is logged and rebuilt by the caller, which still checks the file itself first.
func (s *Service) cachedText(ctx context.Context, id shared.MaterialID, ref shared.ArtifactRef) (material.Text, bool) {
	data, err := s.readArtifact(ctx, ref, maxCachedTextBytes)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return material.Text{}, false
	case err != nil:
		if ctx.Err() == nil {
			s.log.Warn(opTextCacheRead, slog.String("material", string(id)), slog.Any("cause", err))
		}
		return material.Text{}, false
	}
	text, err := material.DecodeCache(data)
	if err != nil {
		s.log.Warn(opTextCacheBroken, slog.String("material", string(id)), slog.Any("cause", err))
		return material.Text{}, false
	}
	return text, true
}

// cacheText writes the text read from a file to its cache; a failed write is logged, the next read extracts again.
func (s *Service) cacheText(ctx context.Context, id shared.MaterialID, ref shared.ArtifactRef, text material.Text) {
	data, err := text.EncodeCache()
	if err == nil {
		err = s.blobs.Write(ctx, ref, bytes.NewReader(data))
	}
	if err != nil && ctx.Err() == nil {
		s.log.Warn(opTextCacheSave, slog.String("material", string(id)), slog.Any("cause", err))
	}
}

// readBlob reads a stored file, at most material.MaxBytes; a larger one is not a file of the library.
func (s *Service) readBlob(ctx context.Context, ref shared.ArtifactRef) ([]byte, error) {
	return s.readArtifact(ctx, ref, material.MaxBytes)
}

// readArtifact reads a stored artifact of at most limit bytes.
func (s *Service) readArtifact(ctx context.Context, ref shared.ArtifactRef, limit int) ([]byte, error) {
	rc, err := s.blobs.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("stored artifact larger than %d bytes", limit)
	}
	return data, nil
}

// Inspect reads the text of a material and asks the quarantined reader INSPECT_MATERIAL (ADR-0022) for its kind,
// subject, lesson date and flags, with the student's subjects as candidates. The answer is checked strictly
// (inspection.Request.Parse) and kept per material and text: asking again about the same text answers from the
// store, free. It is a suggestion only: Inspect files, links and sends nothing; placing a file stays Place and Link.
//
// Without text there is nothing to ask: a PDF answers TextPDFNotExtracted, a photo TextNoText, a file that cannot be
// read TextFailed, a text with nothing in it Empty. The material's text status is recorded once it is known.
//
// Errors: ErrMaterialNotFound, ErrMaterialRemoved, ErrFileDamaged, ErrReaderUnavailable (try again later, free),
// ErrQuota, ErrReaderRefused (the answer broke the schema or the provider refused for good; the attempt is used, the
// next try is a new one, possibly charged, up to inspection.MaxAttempts), ErrUnavailable, ErrNotConfigured.
func (s *Service) Inspect(ctx context.Context, id shared.MaterialID) (Inspected, error) {
	if s.reader == nil || s.inspections == nil {
		return Inspected{}, ErrNotConfigured
	}
	m, text, err := s.MaterialText(ctx, id)
	switch {
	case errors.Is(err, ErrNoText):
		return Inspected{Material: m, Text: m.TextStatus()}, nil
	case errors.Is(err, ErrTextUnreadable):
		return Inspected{Material: s.textStatus(ctx, m, material.TextFailed), Text: material.TextFailed}, nil
	case err != nil:
		return Inspected{}, err
	}
	m = s.textStatus(ctx, m, material.TextExtracted)
	out := Inspected{Material: m, Text: material.TextExtracted, Empty: text.Empty()}
	if out.Empty {
		return out, nil
	}

	subjects, err := s.subjects.List(ctx)
	if err != nil {
		return Inspected{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	cands := make([]inspection.Candidate, 0, len(subjects))
	live := make(map[shared.SubjectID]bool, len(subjects))
	for _, sub := range subjects {
		cands = append(cands, inspection.Candidate{ID: sub.ID(), Name: sub.Name().String()})
		live[sub.ID()] = true
	}

	digest := text.Digest()
	cur, err := s.inspections.Get(ctx, id)
	plan := inspection.First()
	switch {
	case errors.Is(err, shared.ErrNotFound):
		cur = nil
	case err != nil:
		return Inspected{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	default:
		if plan, err = cur.Next(digest); err != nil {
			return Inspected{}, fmt.Errorf("%w: %w", ErrReaderRefused, err)
		}
	}
	if plan.Cached {
		out.Answer = suggestion(plan.Answer, live)
		return out, nil
	}

	req, err := inspection.NewRequest(id, s.clock.Now(), cands, text.Head(inspection.MaxTextBytes), plan.Attempt)
	if err != nil {
		return Inspected{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	raw, err := s.reader.InspectMaterial(ctx, req)
	switch {
	case err == nil:
	case errors.Is(err, shared.ErrQuotaExceeded):
		return Inspected{}, fmt.Errorf("%w: %w", ErrQuota, err)
	case errors.Is(err, inspection.ErrInvalidAnswer):
		// A permanent provider error: the Hub's schema refusal (charged) or a model refusal (not charged); the client
		// cannot tell them apart, so either uses the attempt and the next try is a new one with a new key.
		s.record(ctx, cur, id, digest, plan.Attempt, nil)
		return Inspected{}, fmt.Errorf("%w: %w", ErrReaderRefused, err)
	default:
		// No network, no Hub, a key or a request the Hub refuses: nothing was charged as an answer, the same
		// attempt is asked again later (backoff.ErrPermanent tells only that waiting will not fix it).
		return Inspected{}, fmt.Errorf("%w: %w", ErrReaderUnavailable, err)
	}
	ans, err := req.Parse(raw)
	if err != nil {
		s.record(ctx, cur, id, digest, plan.Attempt, nil)
		return Inspected{}, fmt.Errorf("%w: %w", ErrReaderRefused, err)
	}
	s.record(ctx, cur, id, digest, plan.Attempt, &ans)
	s.log.Info(opInspectAsked, slog.String("material", string(id)), slog.Int("attempt", plan.Attempt))
	out.Answer = suggestion(ans, live)
	return out, nil
}

// MentionsEmail reports whether the quarantined reader said the material names an email address (TASK-127): the
// confirmation of a send then says «В файле указан другой адрес — не используется». It reads only the stored answer,
// never the text, and asks nobody: no answer yet, a refused one or no reader is false. The address itself is never
// kept (ADR-0022).
func (s *Service) MentionsEmail(ctx context.Context, id shared.MaterialID) (bool, error) {
	if s.inspections == nil {
		return false, nil
	}
	cur, err := s.inspections.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return cur.Status() == inspection.StatusDone && cur.Answer().MentionsEmail, nil
}

// suggestion is the answer as the student sees it: a subject removed since it was named is dropped.
func suggestion(a inspection.Answer, live map[shared.SubjectID]bool) *inspection.Answer {
	if a.Subject != "" && !live[a.Subject] {
		a.Subject = ""
	}
	return &a
}

// record keeps the outcome of an attempt. It is a cache: a failed save is logged, and the next call asks again with
// the same key, which the Hub replays without a second charge.
func (s *Service) record(ctx context.Context, cur *inspection.Inspection, id shared.MaterialID, d material.Digest,
	attempt int, ans *inspection.Answer) {
	now := s.clock.Now()
	var err error
	if cur == nil {
		cur, err = inspection.New(id, d, attempt, ans, now)
	} else {
		err = cur.Record(d, attempt, ans, now)
	}
	if err == nil {
		err = s.inspections.Save(ctx, cur)
	}
	if err != nil {
		s.log.Warn(opInspectSave, slog.String("material", string(id)), slog.Any("cause", err))
	}
}

// textStatus records how far the text of m was read, once; a failed save is logged and m returned as it was. It keeps
// the material's version (SaveText), so a place or link before it can still be undone.
func (s *Service) textStatus(ctx context.Context, m *material.Material, st material.TextStatus) *material.Material {
	if m.TextStatus() == st {
		return m
	}
	got, err := s.changeMaterialWith(ctx, m.ID(), func(m *material.Material) (bool, error) {
		changed, err := m.SetText(st)
		return changed, domainError(err)
	}, s.materials.SaveText)
	if err != nil {
		s.log.Warn(opTextStatus, slog.String("material", string(m.ID())), slog.Any("cause", err))
		return m
	}
	return got
}
