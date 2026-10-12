package modelselection

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// Errors of the model selection (ARCH-18).
var (
	// ErrModelNotInstalled means the requested or default model is not installed. It wraps shared.ErrNotFound.
	ErrModelNotInstalled = fmt.Errorf("%w: stt model is not installed", shared.ErrNotFound)

	// ErrNoDefaultModel means a job named no model, no default is selected and the choice is ambiguous.
	ErrNoDefaultModel = errors.New("no default stt model selected")
)

// Selector holds the rules for choosing a model: which model an STT job pins, what happens to the default when a
// model is removed, and which catalog model to recommend for a machine (PRD sections 8 and 8.1). It has no state.
type Selector struct{}

// New returns the selector.
func New() Selector { return Selector{} }

// Resolve picks the model an STT job pins (PRD section 8, AC-08), in this order:
//   - the requested model, which must be installed;
//   - the default model, which must be installed;
//   - the only installed model, when exactly one is installed (there is no choice to make).
//
// Otherwise it returns ErrModelNotInstalled or ErrNoDefaultModel.
func (Selector) Resolve(requested, defaultID shared.SttModelID, installed []*sttmodel.Model) (*sttmodel.Model, error) {
	find := func(id shared.SttModelID) (*sttmodel.Model, error) {
		for _, m := range installed {
			if m.ID() == id {
				return m, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrModelNotInstalled, id)
	}

	switch {
	case requested != "":
		return find(requested)
	case defaultID != "":
		return find(defaultID)
	case len(installed) == 1:
		return installed[0], nil
	case len(installed) == 0:
		return nil, ErrModelNotInstalled
	}
	return nil, ErrNoDefaultModel
}

// DefaultAfterRemoval returns the default model that remains when removed is removed, and whether it changed. Removing
// the default leaves no default: the user picks the next one (PRD section 8, AC-07); nothing is chosen silently.
func (Selector) DefaultAfterRemoval(current, removed shared.SttModelID) (shared.SttModelID, bool) {
	if current == "" || current != removed {
		return current, false
	}
	return "", true
}

// Recommend returns the catalog model to suggest for a machine (PRD sections 8 and 8.1). Only preferred entries (the
// small quantized defaults) are candidates; larger models stay an opt-in choice. On a known machine it picks the
// most demanding preferred entry the machine meets; if it meets none, or the machine is unknown, the least demanding
// preferred entry. Ties are broken by catalog order. ok is false when the catalog has no preferred entry.
func (Selector) Recommend(entries []values.CatalogEntry, m values.Machine) (values.CatalogEntry, bool) {
	var best, lightest values.CatalogEntry
	var haveBest, haveLightest bool
	for _, e := range entries {
		if !e.Recommendation().Preferred {
			continue
		}
		if !haveLightest || demand(e).less(demand(lightest)) {
			lightest, haveLightest = e, true
		}
		if m.Known() && meets(m, e) && (!haveBest || demand(best).less(demand(e))) {
			best, haveBest = e, true
		}
	}

	if haveBest {
		return best, true
	}
	return lightest, haveLightest
}

// LikelySlow reports whether the machine is below the recommended hardware of the entry (PRD section 8: warn when a
// chosen model is likely to be slow). An unknown machine gives no warning.
func (Selector) LikelySlow(e values.CatalogEntry, m values.Machine) bool {
	return m.Known() && !meets(m, e)
}

func meets(m values.Machine, e values.CatalogEntry) bool {
	r := e.Recommendation()
	return m.CPUCores() >= r.MinCPUCores && m.RAMMB() >= r.MinRAMMB
}

// requirement orders entries by how demanding they are: RAM first, then cores, then file size.
type requirement struct {
	ramMB int64
	cores int
	size  int64
}

func demand(e values.CatalogEntry) requirement {
	r := e.Recommendation()
	return requirement{ramMB: r.MinRAMMB, cores: r.MinCPUCores, size: e.SizeBytes()}
}

func (a requirement) less(b requirement) bool {
	if a.ramMB != b.ramMB {
		return a.ramMB < b.ramMB
	}
	if a.cores != b.cores {
		return a.cores < b.cores
	}
	return a.size < b.size
}
