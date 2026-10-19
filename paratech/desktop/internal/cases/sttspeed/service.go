package sttspeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// ErrInvalidConfig is returned by New when a port is missing or the CPU count is not positive.
var ErrInvalidConfig = errors.New("invalid recognition speed configuration")

// Operation names, used in error messages.
const (
	opOverview = "sttspeed: overview"
	opTime     = "sttspeed: transcription time"
)

// Service computes the recognition time estimates of this computer. It changes nothing.
type Service struct {
	d    Deps
	cpus int
	est  speedestimate.Estimator
	sel  modelselection.Selector
}

// New builds the Service for a computer with logicalCPUs logical processors (the count the engine uses).
func New(d Deps, logicalCPUs int) (*Service, error) {
	if d.Models == nil || d.Catalog == nil || d.Speeds == nil || d.Settings == nil || logicalCPUs < 1 {
		return nil, fmt.Errorf("%w: every port and a positive CPU count are required", ErrInvalidConfig)
	}
	return &Service{d: d, cpus: logicalCPUs, est: speedestimate.New(), sel: modelselection.New()}, nil
}

// state is everything one estimate needs, read once per call.
type state struct {
	load      shared.RecognitionLoad
	def       shared.SttModelID
	installed []*sttmodel.Model
	catalog   values.Catalog
	speeds    map[values.SpeedKey]values.MeasuredSpeed
}

func (s *Service) read(ctx context.Context) (state, error) {
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return state{}, err
	}
	installed, err := s.d.Models.List(ctx)
	if err != nil {
		return state{}, err
	}
	data, err := s.d.Catalog.Manifest(ctx)
	if err != nil {
		return state{}, err
	}
	cat, err := values.ParseCatalog(data)
	if err != nil {
		return state{}, err
	}
	speeds, err := s.d.Speeds.Speeds(ctx)
	if err != nil {
		return state{}, err
	}
	return state{load: st.RecognitionLoad(), def: st.DefaultSttModel(), installed: installed, catalog: cat, speeds: speeds}, nil
}

// Overview returns the current mode, the estimates of every catalog entry (with the speed measured for the model
// installed from it) and of every installed model, and the model in use.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	st, err := s.read(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("%s: %w", opOverview, err)
	}

	out := Overview{
		Load: st.load, Catalog: make(map[values.CatalogID]ModelEstimates),
		Installed: make(map[shared.SttModelID]ModelEstimates, len(st.installed)),
	}
	fromEntry := make(map[values.CatalogID]shared.SttModelID)
	for _, m := range st.installed {
		if m.CatalogID() != "" {
			fromEntry[m.CatalogID()] = m.ID()
		}
		out.Installed[m.ID()] = s.estimates(st, m.ID(), familyOf(st.catalog, m), m.Quantization())
	}
	for _, e := range st.catalog.Entries() {
		out.Catalog[e.ID()] = s.estimates(st, fromEntry[e.ID()], e.Family(), e.Quantization())
	}
	if m, err := s.sel.Resolve("", st.def, st.installed); err == nil {
		out.InUse = m.ID()
	}
	return out, nil
}

// TranscriptionTime returns how long recognising audio with the installed model takes in the current mode; ok is
// false when there is no estimate (an imported model never measured, a model no longer installed).
func (s *Service) TranscriptionTime(ctx context.Context, model shared.SttModelID, audio time.Duration) (
	d time.Duration, ok bool, err error) {
	st, err := s.read(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", opTime, err)
	}
	for _, m := range st.installed {
		if m.ID() != model {
			continue
		}
		e := s.estimate(st, model, familyOf(st.catalog, m), m.Quantization(), st.load)
		return e.For(audio), e.Known(), nil
	}
	return 0, false, nil
}

func (s *Service) estimates(st state, model shared.SttModelID, f values.ModelFamily, q values.Quantization) ModelEstimates {
	loads := shared.RecognitionLoads()
	out := ModelEstimates{Modes: make([]speedestimate.Estimate, 0, len(loads))}
	for _, l := range loads {
		e := s.estimate(st, model, f, q, l)
		out.Modes = append(out.Modes, e)
		if l == st.load {
			out.Current = e
		}
	}
	return out
}

// estimate is the estimate of the model in the mode; model may be empty (a catalog entry not installed).
func (s *Service) estimate(st state, model shared.SttModelID, f values.ModelFamily, q values.Quantization,
	l shared.RecognitionLoad) speedestimate.Estimate {
	var measured values.MeasuredSpeed
	if model != "" {
		measured = st.speeds[values.SpeedKey{Model: model, Load: l}]
	}
	return s.est.Estimate(f, q, l, s.cpus, measured)
}

// familyOf is the family of the catalog entry the model was installed from; none for an imported model.
func familyOf(cat values.Catalog, m *sttmodel.Model) values.ModelFamily {
	if e, ok := cat.Entry(m.CatalogID()); ok && m.CatalogID() != "" {
		return e.Family()
	}
	return values.FamilyNone
}
