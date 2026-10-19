package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names of the speed store, used in its error messages.
const (
	opGetSpeed   = "sqlite: get recognition speed"
	opListSpeeds = "sqlite: list recognition speeds"
	opSaveSpeed  = "sqlite: save recognition speed"
)

// maxStoredSamples bounds the count read back before the conversion to int; the domain bounds it further.
const maxStoredSamples = 1 << 31

// SpeedStore keeps the recognition speed measured per installed model and load mode (TASK-084): the speed ports of
// transcribe and sttspeed. Validation stays in values.MeasuredSpeed; a row it refuses reads as not measured.
type SpeedStore struct{ db *DB }

// NewSpeedStore returns the speed store of db.
func NewSpeedStore(db *DB) *SpeedStore { return &SpeedStore{db: db} }

// Speed returns the measured speed of the model in the mode; the zero value when nothing (valid) is stored.
func (r *SpeedStore) Speed(ctx context.Context, key values.SpeedKey) (values.MeasuredSpeed, error) {
	row, err := r.db.Queries(ctx).GetSttSpeed(ctx, sqlcgen.GetSttSpeedParams{
		SttModelID: string(key.Model), RecognitionLoad: string(key.Load),
	})
	if err != nil {
		if err = MapError(ctx, err); errors.Is(err, shared.ErrNotFound) {
			return values.MeasuredSpeed{}, nil
		}
		return values.MeasuredSpeed{}, fmt.Errorf("%s: %w", opGetSpeed, err)
	}
	return speedOf(row.Factor, row.Samples), nil
}

// Speeds returns every valid stored speed.
func (r *SpeedStore) Speeds(ctx context.Context) (map[values.SpeedKey]values.MeasuredSpeed, error) {
	rows, err := r.db.Queries(ctx).ListSttSpeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListSpeeds, MapError(ctx, err))
	}
	out := make(map[values.SpeedKey]values.MeasuredSpeed, len(rows))
	for _, row := range rows {
		if s := speedOf(row.Factor, row.Samples); !s.IsZero() {
			out[values.SpeedKey{Model: shared.SttModelID(row.SttModelID), Load: shared.RecognitionLoad(row.RecognitionLoad)}] = s
		}
	}
	return out, nil
}

// SaveSpeed replaces the speed of the model in the mode. A model that is not installed (any more) yields an error
// wrapping shared.ErrConflict (foreign key).
func (r *SpeedStore) SaveSpeed(ctx context.Context, key values.SpeedKey, s values.MeasuredSpeed, now time.Time) error {
	if s.IsZero() || !key.Load.Valid() {
		return fmt.Errorf("%s: nothing measured or unknown mode %q", opSaveSpeed, key.Load)
	}
	err := r.db.Queries(ctx).UpsertSttSpeed(ctx, sqlcgen.UpsertSttSpeedParams{
		SttModelID: string(key.Model), RecognitionLoad: string(key.Load), Factor: s.Factor(), Samples: int64(s.Samples()),
		UpdatedAt: now.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("%s: %w", opSaveSpeed, MapError(ctx, err))
	}
	return nil
}

func speedOf(factor float64, samples int64) values.MeasuredSpeed {
	if samples < 1 || samples > maxStoredSamples {
		return values.MeasuredSpeed{}
	}
	s, err := values.RestoreMeasuredSpeed(factor, int(samples))
	if err != nil {
		return values.MeasuredSpeed{}
	}
	return s
}
