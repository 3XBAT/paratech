package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names of the model repository and the pin lookup, used in their error messages.
const (
	opGetModel    = "sqlite: get stt model"
	opListModels  = "sqlite: list stt models"
	opSaveModel   = "sqlite: save stt model"
	opDeleteModel = "sqlite: delete stt model"
	opModelPinned = "sqlite: stt model pin lookup"
)

// ModelRepository stores installed STT models (the modelcatalog.ModelRepository port; its Get also serves the
// transcribe.ModelReader port). Validation stays in sttmodel.Model: the repository only converts rows.
type ModelRepository struct{ db *DB }

// NewModelRepository returns the model repository of db.
func NewModelRepository(db *DB) *ModelRepository { return &ModelRepository{db: db} }

// Get loads an installed model; a missing one wraps shared.ErrNotFound.
func (r *ModelRepository) Get(ctx context.Context, id shared.SttModelID) (*sttmodel.Model, error) {
	row, err := r.db.Queries(ctx).GetSttModel(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetModel, id, MapError(ctx, err))
	}

	m, err := modelFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetModel, id, err)
	}
	return m, nil
}

// List returns all installed models ordered by name, model version and id.
func (r *ModelRepository) List(ctx context.Context) ([]*sttmodel.Model, error) {
	rows, err := r.db.Queries(ctx).ListSttModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListModels, MapError(ctx, err))
	}

	out := make([]*sttmodel.Model, 0, len(rows))
	for _, row := range rows {
		m, err := modelFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", opListModels, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// Save writes the model: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). Another model
// with the same name and model version yields shared.ErrDuplicate; an id that exists already, or a stale version,
// yields shared.ErrVersionConflict.
func (r *ModelRepository) Save(ctx context.Context, m *sttmodel.Model) error {
	if m.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveModel, m.Version())
	}
	caps, err := modelCapabilities(m.Capabilities())
	if err != nil {
		return fmt.Errorf("%s: %w", opSaveModel, err)
	}

	next := m.Version() + 1
	p := modelInsertParams(m, caps)
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if m.Version() == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertSttModel(ctx, p)))
		}
		n, err := q.UpdateSttModel(ctx, sqlcgen.UpdateSttModelParams{
			Name: p.Name, ModelVersion: p.ModelVersion, CatalogID: p.CatalogID, FileRef: p.FileRef,
			SizeBytes: p.SizeBytes, Sha256: p.Sha256, Quantization: p.Quantization, Capabilities: p.Capabilities,
			InstalledAt: p.InstalledAt, ID: p.ID, ExpectedVersion: m.Version(),
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveModel, m.ID(), err)
	}

	m.Persisted(next)
	return nil
}

// Delete removes the model row. A missing row wraps shared.ErrNotFound; a model that is still the settings default
// wraps shared.ErrConflict (ON DELETE RESTRICT, ADR-0004(g)). Transcripts keep their copied name and version.
func (r *ModelRepository) Delete(ctx context.Context, id shared.SttModelID) error {
	err := r.db.Do(ctx, func(ctx context.Context) error {
		n, err := r.db.Queries(ctx).DeleteSttModel(ctx, string(id))
		if err != nil {
			return MapError(ctx, err)
		}
		if n == 0 {
			return shared.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteModel, id, err)
	}
	return nil
}

// ModelPins answers whether an unfinished job pins a model (the modelcatalog.ModelPins port).
type ModelPins struct{ db *DB }

// NewModelPins returns the pin lookup of db.
func NewModelPins(db *DB) *ModelPins { return &ModelPins{db: db} }

// IsPinnedByJobInStatuses reports whether a job in one of the statuses pins the model. No status means no pin.
func (p *ModelPins) IsPinnedByJobInStatuses(ctx context.Context, id shared.SttModelID, statuses []job.Status) (bool, error) {
	if len(statuses) == 0 {
		return false, nil
	}
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}

	pinned, err := p.db.Queries(ctx).IsSttModelPinned(ctx, sqlcgen.IsSttModelPinnedParams{
		ModelID: sql.NullString{String: string(id), Valid: true}, Statuses: names,
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opModelPinned, id, MapError(ctx, err))
	}
	return pinned, nil
}

func modelFromRow(row sqlcgen.SttModel) (*sttmodel.Model, error) {
	file, err := shared.ParseArtifactRef(row.FileRef)
	if err != nil {
		return nil, fmt.Errorf("stored model %s: %w", row.ID, err)
	}
	var labels []string
	if err := json.Unmarshal([]byte(row.Capabilities), &labels); err != nil {
		return nil, fmt.Errorf("stored model %s: capabilities: %w", row.ID, err)
	}
	caps := make([]values.Capability, len(labels))
	for i, c := range labels {
		caps[i] = values.Capability(c)
	}

	m, err := sttmodel.Restore(sttmodel.State{
		Spec: sttmodel.Spec{
			ID: shared.SttModelID(row.ID), Name: values.ModelName(row.Name),
			ModelVersion: values.ModelVersion(row.ModelVersion), CatalogID: values.CatalogID(row.CatalogID.String),
			File: file, SizeBytes: row.SizeBytes, SHA256: values.SHA256(row.Sha256),
			Quantization: values.Quantization(row.Quantization.String), Capabilities: caps,
		},
		Version:     row.Version,
		InstalledAt: time.UnixMilli(row.InstalledAt).UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("stored model %s: %w", row.ID, err)
	}
	return m, nil
}

func modelInsertParams(m *sttmodel.Model, caps string) sqlcgen.InsertSttModelParams {
	return sqlcgen.InsertSttModelParams{
		ID:           string(m.ID()),
		Name:         string(m.Name()),
		ModelVersion: string(m.ModelVersion()),
		CatalogID:    modelNullString(string(m.CatalogID())),
		FileRef:      m.File().String(),
		SizeBytes:    m.SizeBytes(),
		Sha256:       string(m.SHA256()),
		Quantization: modelNullString(string(m.Quantization())),
		Capabilities: caps,
		InstalledAt:  m.InstalledAt().UnixMilli(),
	}
}

// modelCapabilities encodes the labels as a JSON array; never "null", which the column default does not expect.
func modelCapabilities(caps []values.Capability) (string, error) {
	labels := make([]string, len(caps))
	for i, c := range caps {
		labels[i] = string(c)
	}
	b, err := json.Marshal(labels)
	return string(b), err
}

func modelNullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
